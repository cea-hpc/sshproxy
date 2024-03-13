// Copyright 2015-2024 CEA/DAM/DIF
//  Author: Arnaud Guignard <arnaud.guignard@cea.fr>
//  Contributor: Cyril Servant <cyril.servant@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/op/go-logging"
	"go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// State of a host.
type State int

// These are the possible states of an host:
//
//	Up: host is up,
//	Down: host is down,
//	Disabled: host was disabled by an admin.
const (
	Unknown State = iota
	Up
	Down
	Disabled
)

var (
	keyRegex = regexp.MustCompile(`^([^@]+)@([^:]+)$`)
)

func (s State) String() string {
	switch s {
	default:
		return "unknown"
	case Up:
		return "up"
	case Down:
		return "down"
	case Disabled:
		return "disabled"
	}
}

// UnmarshalJSON translates a JSON representation into a state or returns an
// error.
func (s *State) UnmarshalJSON(b []byte) error {
	var t string
	if err := json.Unmarshal(b, &t); err != nil {
		return err
	}
	switch strings.ToLower(t) {
	default:
		*s = Unknown
	case "up":
		*s = Up
	case "down":
		*s = Down
	case "disabled":
		*s = Disabled
	}
	return nil
}

// MarshalJSON translates a state into a JSON representation or returns an
// error.
func (s State) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

var (
	etcdRootPath        = "/sshproxy"
	etcdConnectionsPath = etcdRootPath + "/connections"
	etcdHistoryPath     = etcdRootPath + "/history"
	etcdHostsPath       = etcdRootPath + "/hosts"

	// ErrKeyNotFound is returned when key is not found in etcd.
	ErrKeyNotFound = errors.New("key not found")
)

func toConnectionKey(d string) string {
	return fmt.Sprintf("%s/%s", etcdConnectionsPath, d)
}

func toHistoryKey(d string) string {
	return fmt.Sprintf("%s/%s", etcdHistoryPath, d)
}

func toHostKey(h string) string {
	return fmt.Sprintf("%s/%s", etcdHostsPath, h)
}

// Client is a wrapper to easily do request to etcd cluster.
type Client struct {
	cli            *clientv3.Client
	log            *logging.Logger
	requestTimeout time.Duration
	keyTTL         int64
	active         bool
	leaseID        clientv3.LeaseID
}

// Host represents the state of a host.
type Host struct {
	State State     // host state (see State const for available states)
	Ts    time.Time // time of last check
}

// Bandwidth represents the amount of kB/s
type Bandwidth struct {
	In  int // stdin
	Out int // stdout + stderr
}

// NewEtcdClient creates a new etcd client.
func NewEtcdClient(config *Config, log *logging.Logger) (*Client, error) {
	var tlsConfig *tls.Config

	if config.Etcd.TLS.CertFile != "" && config.Etcd.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.Etcd.TLS.CertFile, config.Etcd.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("configuring TLS for etcd: %v", err)
		}
		cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

		if config.Etcd.TLS.CAFile != "" {
			cfg.RootCAs, err = newCertPool(config.Etcd.TLS.CAFile)
			if err != nil {
				return nil, fmt.Errorf("configuring TLS for etcd: %v", err)
			}
		}

		tlsConfig = cfg
	}

	cli, err := clientv3.New(clientv3.Config{
		DialTimeout: 2 * time.Second,
		Endpoints:   config.Etcd.Endpoints,
		TLS:         tlsConfig,
		Username:    config.Etcd.Username,
		Password:    config.Etcd.Password,
		DialOptions: []grpc.DialOption{grpc.WithBlock()},
		LogConfig: &zap.Config{
			Level:            zap.NewAtomicLevelAt(zap.ErrorLevel),
			Encoding:         "json",
			OutputPaths:      []string{"/dev/null"},
			ErrorOutputPaths: []string{"/dev/null"},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("creating etcd client: %v", err)
	}

	keyTTL := config.Etcd.KeyTTL
	if keyTTL == 0 {
		keyTTL = 5
	}

	return &Client{
		cli:            cli,
		log:            log,
		requestTimeout: 2 * time.Second,
		keyTTL:         keyTTL,
		active:         true,
	}, nil
}

// NewCertPool creates x509 certPool with provided CA files.
func newCertPool(CAFile string) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()

	pemByte, err := os.ReadFile(CAFile)
	if err != nil {
		return nil, err
	}

	for {
		var block *pem.Block
		block, pemByte = pem.Decode(pemByte)
		if block == nil {
			break
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certPool.AddCert(cert)
	}

	return certPool, nil
}

// Close terminates the etcd client.
func (c *Client) Close() {
	if c.cli != nil {
		c.cli.Close()
		c.cli = nil
	}
}

// GetDestination returns the destination found in etcd for a user connected to
// an SSH daemon (key). If the key is not present and etcdKeyTTL is defined,
// the key is searched in history. If it's not found, the error will be
// etcd.ErrKeyNotFound.
func (c *Client) GetDestination(key string, etcdKeyTTL int64) (string, error) {
	path := toConnectionKey(key)
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, path, clientv3.WithPrefix(), clientv3.WithKeysOnly(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortDescend))
	cancel()
	if err != nil {
		return "", err
	}

	if len(resp.Kvs) == 0 {
		if etcdKeyTTL > 0 {
			history := toHistoryKey(key)
			ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
			resp, err := c.cli.Get(ctx, history, clientv3.WithPrefix())
			cancel()
			if err != nil {
				return "", err
			}

			if len(resp.Kvs) != 0 {
				return string(resp.Kvs[0].Value), nil
			}
		}
		return "", ErrKeyNotFound
	}

	subkey := string(resp.Kvs[0].Key)[len(path)+1:]
	dest := strings.SplitN(subkey, "/", 2)
	return dest[0], nil
}

func (c *Client) getExistingLease(key string) (string, error) {
	history := toHistoryKey(key)
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, history, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	cancel()
	if err != nil {
		return "", err
	}

	if len(resp.Kvs) == 0 {
		return "", ErrKeyNotFound
	}

	lease := string(resp.Kvs[0].Key)[len(history)+1:]
	return lease, nil
}

// SetDestination set current destination in etcd.
func (c *Client) SetDestination(rootctx context.Context, key, sshdHostport string, dst string, etcdKeyTTL int64) (<-chan *clientv3.LeaseKeepAliveResponse, string, error) {
	path := fmt.Sprintf("%s/%s/%s/%s", toConnectionKey(key), dst, sshdHostport, time.Now().Format(time.RFC3339Nano))
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	var history string
	var historyID clientv3.LeaseID
	if etcdKeyTTL > 0 {
		lease, err := c.getExistingLease(key)
		if err == nil {
			tmpHistoryID, _ := strconv.Atoi(lease)
			historyID = clientv3.LeaseID(tmpHistoryID)
		} else {
			respHistory, err := c.cli.Grant(ctx, etcdKeyTTL)
			if err == nil {
				historyID = respHistory.ID
			}
		}
		history = fmt.Sprintf("%s/%d", toHistoryKey(key), int64(historyID))
	}
	resp, err := c.cli.Grant(ctx, c.keyTTL)
	cancel()
	if err != nil {
		return nil, "", err
	}

	bytes, err := json.Marshal(&Bandwidth{
		In:  0,
		Out: 0,
	})
	if err != nil {
		return nil, "", err
	}
	ctx, cancel = context.WithTimeout(context.Background(), c.requestTimeout)
	_, err = c.cli.Put(ctx, path, string(bytes), clientv3.WithLease(resp.ID))
	if etcdKeyTTL > 0 {
		_, err = c.cli.Put(ctx, history, dst, clientv3.WithLease(historyID))
	}
	cancel()
	if err != nil {
		return nil, "", err
	}

	k, e := c.cli.KeepAlive(rootctx, resp.ID)
	if etcdKeyTTL > 0 {
		c.cli.KeepAlive(rootctx, historyID)
	}
	c.leaseID = resp.ID
	return k, path, e
}

// NewLease creates a new lease in etcd.
func (c *Client) NewLease(rootctx context.Context) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Grant(ctx, c.keyTTL)
	cancel()
	if err != nil {
		return nil, err
	}

	k, e := c.cli.KeepAlive(rootctx, resp.ID)
	c.leaseID = resp.ID
	return k, e
}

// UpdateStats updates the stats (bandwidth in and out in kB/s) of a connection.
func (c *Client) UpdateStats(etcdPath string, stats map[int]uint64) error {
	bytes, err := json.Marshal(&Bandwidth{
		In:  int(stats[0] / 1024),
		Out: int((stats[1] + stats[2]) / 1024),
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	_, err = c.cli.Put(ctx, etcdPath, string(bytes), clientv3.WithLease(c.leaseID))
	cancel()
	if err != nil {
		return err
	}

	return nil
}

// GetHost returns the host (passed as "host:port") details. If host is not
// present the error will be etcd.ErrKeyNotFound.
func (c *Client) GetHost(hostport string) (*Host, error) {
	var h Host
	key := toHostKey(hostport)

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, key)
	cancel()
	if err != nil {
		return nil, err
	}

	switch len(resp.Kvs) {
	case 0:
		return nil, ErrKeyNotFound
	case 1:
		if err := json.Unmarshal([]byte(resp.Kvs[0].Value), &h); err != nil {
			return nil, fmt.Errorf("decoding JSON data at '%s': %v", key, err)
		}
		return &h, nil
	default:
		return nil, fmt.Errorf("got multiple responses for %s", key)
	}

	// not reached
}

// DelHost deletes a host (passed as "host:port") in etcd.
func (c *Client) DelHost(hostport string) error {
	key := toHostKey(hostport)
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	_, err := c.cli.Delete(ctx, key)
	cancel()
	if err != nil {
		return err
	}
	return nil
}

// SetHost sets a host (passed as "host:port") state and last checked time (ts)
// in etcd.
func (c *Client) SetHost(hostport string, state State, ts time.Time) error {
	bytes, err := json.Marshal(&Host{
		State: state,
		Ts:    ts,
	})
	if err != nil {
		return err
	}
	key := toHostKey(hostport)
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	_, err = c.cli.Put(ctx, key, string(bytes))
	cancel()
	if err != nil {
		return err
	}
	return nil
}

// GetErrorBanner returns the current error banner. If error banner is not
// present an empty string will be returned, without error.
func (c *Client) GetErrorBanner() (string, string, error) {
	key := "/sshproxy/error_banner/value"
	keyExpire := "/sshproxy/error_banner/expire"

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, key)
	expire, err2 := c.cli.Get(ctx, keyExpire)
	cancel()
	if err != nil {
		return "", "", err
	}
	if err2 != nil {
		return "", "", err2
	}

	switch len(resp.Kvs) {
	case 0:
		return "", "", nil
	case 1:
		switch len(expire.Kvs) {
		case 0:
			return string(resp.Kvs[0].Value), "", nil
		case 1:
			return string(resp.Kvs[0].Value), string(expire.Kvs[0].Value), nil
		default:
			return "", "", fmt.Errorf("got multiple responses for %s", keyExpire)
		}
	default:
		return "", "", fmt.Errorf("got multiple responses for %s", key)
	}

	// not reached
}

// DelErrorBanner deletes the error banner in etcd.
func (c *Client) DelErrorBanner() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	_, err := c.cli.Delete(ctx, "/sshproxy/error_banner/value")
	_, err2 := c.cli.Delete(ctx, "/sshproxy/error_banner/expire")
	cancel()
	if err != nil {
		return err
	}
	if err2 != nil {
		return err2
	}
	return nil
}

// SetErrorBanner sets the error banner in etcd during a given time.
func (c *Client) SetErrorBanner(errorBanner string, expire time.Time) error {
	key := "/sshproxy/error_banner/value"
	keyExpire := "/sshproxy/error_banner/expire"
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()
	currentTime := time.Now()
	diff := expire.Sub(currentTime)
	seconds := int64(diff.Seconds())
	if seconds > 0 {
		resp, err := c.cli.Grant(context.TODO(), seconds)
		if err != nil {
			return err
		}
		_, err = c.cli.Put(ctx, key, errorBanner, clientv3.WithLease(resp.ID))
		_, err2 := c.cli.Put(ctx, keyExpire, expire.Format("2006-01-02 15:04:05"), clientv3.WithLease(resp.ID))
		if err != nil {
			return err
		}
		if err2 != nil {
			return err2
		}
	} else {
		_, err := c.cli.Put(ctx, key, errorBanner)
		_, err2 := c.cli.Delete(ctx, keyExpire)
		if err != nil {
			return err
		}
		if err2 != nil {
			return err2
		}
	}
	return nil
}

// FlatConnection is a structure used to flatten a connection informations
// present in etcd.
type FlatConnection struct {
	User    string
	Service string
	From    string
	Dest    string
	Ts      time.Time
	BwIn    int
	BwOut   int
}

// GetAllConnections returns a list of all connections present in etcd.
func (c *Client) GetAllConnections() ([]*FlatConnection, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, etcdConnectionsPath, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	cancel()
	if err != nil {
		return nil, err
	}

	conns := make([]*FlatConnection, len(resp.Kvs))
	for i, ev := range resp.Kvs {
		v := &FlatConnection{}
		subkey := string(ev.Key)[len(etcdConnectionsPath)+1:]
		fields := strings.Split(subkey, "/")
		if len(fields) != 4 {
			return nil, fmt.Errorf("bad key format %s", subkey)
		}

		userservice := fields[0]
		v.Dest = fields[1]
		v.From = fields[2]
		v.Ts, err = time.Parse(time.RFC3339Nano, fields[3])
		if err != nil {
			return nil, fmt.Errorf("error parsing time %s", fields[2])
		}

		m := keyRegex.FindStringSubmatch(userservice)
		if m == nil || len(m) != 3 {
			return nil, fmt.Errorf("error parsing key %s", userservice)
		}
		v.User, v.Service = m[1], m[2]
		b := &Bandwidth{}
		if err := json.Unmarshal(ev.Value, b); err != nil {
			return nil, fmt.Errorf("decoding JSON data at '%s': %v", ev.Key, err)
		}
		v.BwIn = b.In
		v.BwOut = b.Out
		conns[i] = v
	}

	return conns, nil
}

// GetUserConnectionsCount returns the number of active connections of a user, based on etcd.
func (c *Client) GetUserConnectionsCount(username string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, etcdConnectionsPath, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	cancel()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, ev := range resp.Kvs {
		subkey := string(ev.Key)[len(etcdConnectionsPath)+1:]
		fields := strings.Split(subkey, "/")
		if len(fields) != 4 {
			return 0, fmt.Errorf("bad key format %s", subkey)
		}

		subfields := strings.Split(fields[0], "@")
		if len(subfields) != 2 {
			return 0, fmt.Errorf("bad subkey format %s", fields[0])
		}
		if subfields[0] == username {
			count++
		}
	}

	return count, nil
}

// FlatHost is a structure used to flatten a host informations present in etcd.
type FlatHost struct {
	Hostname string
	N        int
	BwIn     int
	BwOut    int
	*Host
}

// GetUserHosts returns a list of hosts used by a user@service, based on etcd.
func (c *Client) GetUserHosts(key string) (map[string]*FlatHost, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, etcdConnectionsPath, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	cancel()
	if err != nil {
		return nil, err
	}

	hosts := map[string]*FlatHost{}
	for _, ev := range resp.Kvs {
		subkey := string(ev.Key)[len(etcdConnectionsPath)+1:]
		fields := strings.Split(subkey, "/")
		if len(fields) != 4 {
			return nil, fmt.Errorf("bad key format %s", subkey)
		}

		if fields[0] == key {
			b := &Bandwidth{}
			if err := json.Unmarshal(ev.Value, b); err != nil {
				return nil, fmt.Errorf("decoding JSON data at '%s': %v", ev.Key, err)
			}
			if hosts[fields[1]] == nil {
				v := &FlatHost{}
				v.Hostname = fields[1]
				v.N = 1
				v.BwIn = b.In
				v.BwOut = b.Out
				hosts[fields[1]] = v
			} else {
				hosts[fields[1]].N++
				hosts[fields[1]].BwIn += b.In
				hosts[fields[1]].BwOut += b.Out
			}
		}
	}

	return hosts, nil
}

// GetAllHosts returns a list of all hosts present in etcd.
func (c *Client) GetAllHosts() ([]*FlatHost, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, etcdHostsPath, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	cancel()
	if err != nil {
		return nil, err
	}

	connections, err := c.GetAllConnections()
	if err != nil {
		return nil, fmt.Errorf("ERROR: getting connections from etcd: %v", err)
	}
	stats := map[string]map[string]int{}
	for _, connection := range connections {
		if stats[connection.Dest] == nil {
			stats[connection.Dest] = map[string]int{}
			stats[connection.Dest]["N"] = 1
			stats[connection.Dest]["BwIn"] = connection.BwIn
			stats[connection.Dest]["BwOut"] = connection.BwOut
		} else {
			stats[connection.Dest]["N"]++
			stats[connection.Dest]["BwIn"] += connection.BwIn
			stats[connection.Dest]["BwOut"] += connection.BwOut
		}
	}

	hosts := make([]*FlatHost, len(resp.Kvs))
	for i, ev := range resp.Kvs {
		v := &FlatHost{}
		if err := json.Unmarshal([]byte(ev.Value), v); err != nil {
			return nil, fmt.Errorf("decoding JSON data at '%s': %v", ev.Key, err)
		}
		subkey := string(ev.Key)[len(etcdHostsPath)+1:]
		v.Hostname = subkey
		if stats[subkey] == nil {
			v.N = 0
			v.BwIn = 0
			v.BwOut = 0
		} else {
			v.N = stats[subkey]["N"]
			v.BwIn = stats[subkey]["BwIn"]
			v.BwOut = stats[subkey]["BwOut"]
		}
		hosts[i] = v
	}

	return hosts, nil
}

// FlatUser is a structure used to flatten a user informations present in etcd.
type FlatUser struct {
	User    string
	Service string
	Groups  string
	N       int
	BwIn    int
	BwOut   int
}

// GetAllUsers returns a list of connections present in etcd, aggregated by
// user@service.
func (c *Client) GetAllUsers(allFlag bool) ([]*FlatUser, error) {
	connections, err := c.GetAllConnections()
	if err != nil {
		return nil, fmt.Errorf("ERROR: getting connections from etcd: %v", err)
	}
	users := map[string]*FlatUser{}
	for _, connection := range connections {
		key := connection.User
		if allFlag {
			key = fmt.Sprintf("%s@%s", connection.User, connection.Service)
		}
		if users[key] == nil {
			v := &FlatUser{}
			groups, err := GetGroupList(connection.User)
			if err != nil {
				return nil, err
			}
			g := make([]string, 0, len(groups))
			for group := range groups {
				g = append(g, group)
			}
			sort.Strings(g)
			v.Groups = strings.Join(g, " ")
			v.N = 1
			v.BwIn = connection.BwIn
			v.BwOut = connection.BwOut
			users[key] = v
		} else {
			users[key].N++
			users[key].BwIn += connection.BwIn
			users[key].BwOut += connection.BwOut
		}
	}

	usersSlice := make([]*FlatUser, len(users))
	i := 0
	for k, v := range users {
		userService := strings.Split(k, "@")
		usersSlice[i] = v
		usersSlice[i].User = userService[0]
		if allFlag {
			usersSlice[i].Service = userService[1]
		}
		i++
	}

	return usersSlice, nil
}

// FlatGroup is a structure used to flatten a group informations present in etcd.
type FlatGroup struct {
	Group   string
	Service string
	Users   string
	N       int
	BwIn    int
	BwOut   int
}

// GetAllGroups returns a list of connections present in etcd, aggregated by
// groups.
func (c *Client) GetAllGroups(allFlag bool) ([]*FlatGroup, error) {
	users, err := c.GetAllUsers(allFlag)
	if err != nil {
		return nil, fmt.Errorf("ERROR: getting connections from etcd: %v", err)
	}
	groupUsers := map[string]map[string]bool{}
	groups := map[string]*FlatGroup{}
	for _, user := range users {
		for _, group := range strings.Split(user.Groups, " ") {
			if allFlag {
				group += "@" + user.Service
			}
			if groupUsers[group] == nil {
				groupUsers[group] = map[string]bool{}
			}
			groupUsers[group][user.User] = true
			if groups[group] == nil {
				v := &FlatGroup{}
				v.N = user.N
				v.BwIn = user.BwIn
				v.BwOut = user.BwOut
				groups[group] = v
			} else {
				groups[group].N += user.N
				groups[group].BwIn += user.BwIn
				groups[group].BwOut += user.BwOut
			}
		}
	}
	for g, userGroup := range groupUsers {
		u := make([]string, 0, len(userGroup))
		for user := range userGroup {
			u = append(u, user)
		}
		sort.Strings(u)
		groups[g].Users = strings.Join(u, " ")
	}

	groupsSlice := make([]*FlatGroup, len(groups))
	i := 0
	for k, v := range groups {
		groupService := strings.Split(k, "@")
		groupsSlice[i] = v
		groupsSlice[i].Group = groupService[0]
		if allFlag {
			groupsSlice[i].Service = groupService[1]
		}
		i++
	}

	return groupsSlice, nil
}

// IsAlive checks if etcd client is still usable.
func (c *Client) IsAlive() bool {
	return c.cli != nil && c.active
}

// Enable enables the etcd client.
func (c *Client) Enable() {
	c.active = true
}

// Disable disables the etcd client.
func (c *Client) Disable() {
	c.active = false
}
