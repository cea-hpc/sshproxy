// Copyright 2015-2020 CEA/DAM/DIF
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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/op/go-logging"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/pkg/transport"
)

// State of a host.
type State int

// These are the possible states of an host:
//   Up: host is up,
//   Down: host is down,
//   Disabled: host was disabled by an admin.
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
	etcdHostsPath       = etcdRootPath + "/hosts"

	// ErrKeyNotFound is returned when key is not found in etcd.
	ErrKeyNotFound = errors.New("key not found")
)

func toConnectionKey(d string) string {
	return fmt.Sprintf("%s/%s", etcdConnectionsPath, d)
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
	var pTLSInfo *transport.TLSInfo
	tlsInfo := transport.TLSInfo{}

	if config.Etcd.TLS.CertFile != "" {
		tlsInfo.CertFile = config.Etcd.TLS.CertFile
		pTLSInfo = &tlsInfo
	}

	if config.Etcd.TLS.KeyFile != "" {
		tlsInfo.KeyFile = config.Etcd.TLS.KeyFile
		pTLSInfo = &tlsInfo
	}

	if config.Etcd.TLS.CAFile != "" {
		tlsInfo.TrustedCAFile = config.Etcd.TLS.CAFile
		pTLSInfo = &tlsInfo
	}

	if pTLSInfo != nil {
		var err error
		tlsConfig, err = tlsInfo.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("configuring TLS for etcd: %v", err)
		}
	}

	cli, err := clientv3.New(clientv3.Config{
		DialTimeout: 2 * time.Second,
		Endpoints:   config.Etcd.Endpoints,
		TLS:         tlsConfig,
		Username:    config.Etcd.Username,
		Password:    config.Etcd.Password,
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

// Close terminates the etcd client.
func (c *Client) Close() {
	if c.cli != nil {
		c.cli.Close()
		c.cli = nil
	}
}

// GetDestination returns the destination found in etcd for a user connected to
// an SSH daemon (key). If the key is not present the error will be
// etcd.ErrKeyNotFound.
func (c *Client) GetDestination(key string) (string, error) {
	path := toConnectionKey(key)
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, path, clientv3.WithPrefix(), clientv3.WithKeysOnly(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortDescend))
	cancel()
	if err != nil {
		return "", err
	}

	if len(resp.Kvs) == 0 {
		return "", ErrKeyNotFound
	}

	subkey := string(resp.Kvs[0].Key)[len(path)+1:]
	dest := strings.SplitN(subkey, "/", 2)
	return dest[0], nil
}

// SetDestination set current destination in etcd.
func (c *Client) SetDestination(rootctx context.Context, key, sshdHostport string, dst string) (<-chan *clientv3.LeaseKeepAliveResponse, string, error) {
	path := fmt.Sprintf("%s/%s/%s/%s", toConnectionKey(key), dst, sshdHostport, time.Now().Format(time.RFC3339Nano))
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
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
	cancel()
	if err != nil {
		return nil, "", err
	}

	k, e := c.cli.KeepAlive(rootctx, resp.ID)
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

// FlatHost is a structure used to flatten a host informations present in etcd.
type FlatHost struct {
	Hostname string
	Port     string
	N        int
	BwIn     int
	BwOut    int
	*Host
}

// GetUserHosts returns a list of hosts used by a user, based on etcd.
func (c *Client) GetUserHosts(key string) (map[string]*FlatHost, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, etcdConnectionsPath, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	cancel()
	if err != nil {
		return nil, err
	}

	hosts := map[string]*FlatHost{}
	//hosts := make([]*FlatHost, len(resp.Kvs))
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
				v.Hostname, v.Port, err = net.SplitHostPort(fields[1])
				if err != nil {
					return nil, fmt.Errorf("splitting Host and Port from '%s': %v", fields[1], err)
				}
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
		v.Hostname, v.Port, err = net.SplitHostPort(subkey)
		if err != nil {
			return nil, fmt.Errorf("error parsing key %s", subkey)
		}
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
	Groups string
	N      int
	BwIn   int
	BwOut  int
}

// GetAllUsers returns a list of connections present in etcd, aggregated by
// user@service.
func (c *Client) GetAllUsers(allFlag bool) (map[string]*FlatUser, error) {
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

	return users, nil
}

// FlatGroup is a structure used to flatten a group informations present in etcd.
type FlatGroup struct {
	Users string
	N     int
	BwIn  int
	BwOut int
}

// GetAllGroups returns a list of connections present in etcd, aggregated by
// groups.
func (c *Client) GetAllGroups(allFlag bool) (map[string]*FlatGroup, error) {
	users, err := c.GetAllUsers(allFlag)
	if err != nil {
		return nil, fmt.Errorf("ERROR: getting connections from etcd: %v", err)
	}
	groupUsers := map[string]map[string]bool{}
	groups := map[string]*FlatGroup{}
	for user, userV := range users {
		userService := []string{}
		if allFlag {
			userService = strings.Split(user, "@")
			user = userService[0]
		}
		for _, group := range strings.Split(userV.Groups, " ") {
			if allFlag {
				group += "@" + userService[1]
			}
			if groupUsers[group] == nil {
				groupUsers[group] = map[string]bool{}
			}
			groupUsers[group][user] = true
			if groups[group] == nil {
				v := &FlatGroup{}
				v.N = userV.N
				v.BwIn = userV.BwIn
				v.BwOut = userV.BwOut
				groups[group] = v
			} else {
				groups[group].N += userV.N
				groups[group].BwIn += userV.BwIn
				groups[group].BwOut += userV.BwOut
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

	return groups, nil
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
