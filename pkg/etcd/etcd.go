// Copyright 2015-2019 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package etcd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/cea-hpc/sshproxy/pkg/utils"

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
	keyRegex = regexp.MustCompile(`^([^@]+)@([^:]+):([0-9]+)$`)
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
}

// Connection represents the details of a proxied connection for a couple
// (user, host).
type Connection struct {
	Dest string    // Chosen destination
	N    int       // Number of connections
	Ts   time.Time // Start of last connection
}

// Host represent the state of a host.
type Host struct {
	State State     // host state (see State const for available states)
	Ts    time.Time // time of last check
}

// NewClient creates a new etcd client.
func NewClient(config *utils.Config, log *logging.Logger) (*Client, error) {
	var tlsConfig *tls.Config
	if config.Etcd.TLS.CertFile != "" && config.Etcd.TLS.KeyFile != "" && config.Etcd.TLS.CAFile != "" {
		tlsInfo := transport.TLSInfo{
			CertFile:      config.Etcd.TLS.CertFile,
			KeyFile:       config.Etcd.TLS.KeyFile,
			TrustedCAFile: config.Etcd.TLS.CAFile,
		}

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

	return &Client{
		cli:            cli,
		log:            log,
		requestTimeout: 2 * time.Second,
	}, nil
}

// Close terminates the etcd client.
func (c *Client) Close() {
	if c.cli != nil {
		c.cli.Close()
		c.cli = nil
	}
}

func marshalConnection(c *Connection) (string, error) {
	bytes, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// Increment increments the number of connections from a user connected to an
// SSH daemon (key) to a destination dst. A new entry will be created if key is
// not present in etcd.
func (c *Client) Increment(key, dst string) error {
	for {
		if ok, err := c.doIncrement(key, dst); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
}

func (c *Client) doIncrement(key, dest string) (bool, error) {
	path := toConnectionKey(key)
	var conn Connection
	modRevision, err := c.get(path, &conn)
	if err != nil {
		if err == ErrKeyNotFound {
			value, err := marshalConnection(&Connection{
				Dest: dest,
				N:    1,
				Ts:   time.Now(),
			})
			if err != nil {
				return false, err
			}

			// key missing <=> Version = 0
			ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
			resp, err := c.cli.Txn(ctx).
				If(clientv3.Compare(clientv3.Version(path), "=", 0)).
				Then(clientv3.OpPut(path, value)).
				Commit()
			cancel()
			return resp.Succeeded, err
		}
		return false, err
	}

	conn.N++
	conn.Ts = time.Now()
	value, err := marshalConnection(&conn)
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(path), "=", modRevision)).
		Then(clientv3.OpPut(path, value)).
		Commit()
	cancel()
	return resp.Succeeded, err
}

// Decrement decrements the number of connection of a user connected to an SSH
// daemon (key). It removes the key if the number of connections is 0.
func (c *Client) Decrement(key string) error {
	for {
		if ok, err := c.doDecrement(key); err != nil {
			c.Close()
			return err
		} else if ok {
			c.Close()
			return nil
		}
	}
}

func (c *Client) doDecrement(key string) (bool, error) {
	path := toConnectionKey(key)
	var conn Connection
	modRevision, err := c.get(path, &conn)
	if err != nil {
		if err == ErrKeyNotFound {
			if c.log != nil {
				c.log.Errorf("decrementing %s: key does not exist", key)
			}
			return true, nil
		}
		return false, err
	}

	conn.N--

	if conn.N == 0 {
		if c.log != nil {
			c.log.Info("no more active connection for %s (to %s): removing", key, conn.Dest)
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
		resp, err := c.cli.Txn(ctx).
			If(clientv3.Compare(clientv3.ModRevision(path), "=", modRevision)).
			Then(clientv3.OpDelete(path)).
			Commit()
		cancel()
		return resp.Succeeded, err
	}

	value, err := marshalConnection(&conn)
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(path), "=", modRevision)).
		Then(clientv3.OpPut(path, value)).
		Commit()
	cancel()
	return resp.Succeeded, err
}

// get is used by exported functions to get the value associated to a key.
func (c *Client) get(key string, v interface{}) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, key)
	cancel()
	if err != nil {
		return 0, err
	}

	switch len(resp.Kvs) {
	case 0:
		return 0, ErrKeyNotFound
	case 1:
		if err := json.Unmarshal([]byte(resp.Kvs[0].Value), v); err != nil {
			return 0, fmt.Errorf("decoding JSON data at '%s': %v", key, err)
		}
		return resp.Kvs[0].ModRevision, nil
	default:
		return 0, fmt.Errorf("got multiple responses for %s", key)
	}

	// not reached
}

// GetDestination returns the destination found in etcd for a user connected to
// an SSH daemon (key). If the key is not present the error will be
// etcd.ErrKeyNotFound.
func (c *Client) GetDestination(key string) (string, error) {
	var conn Connection
	path := toConnectionKey(key)
	if _, err := c.get(path, &conn); err != nil {
		return "", err
	}
	return conn.Dest, nil
}

// GetHost returns the host (passed as "host:port") details. It host if not
// present the error will be etcd.ErrKeyNotFound.
func (c *Client) GetHost(hostport string) (*Host, error) {
	var h Host
	key := toHostKey(hostport)
	if _, err := c.get(key, &h); err != nil {
		return nil, err
	}
	return &h, nil
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
	User string
	Host string
	Port string
	*Connection
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
		if err := json.Unmarshal([]byte(ev.Value), v); err != nil {
			return nil, fmt.Errorf("decoding JSON data at '%s': %v", ev.Key, err)
		}
		subkey := string(ev.Key)[len(etcdConnectionsPath)+1:]
		m := keyRegex.FindStringSubmatch(subkey)
		if m == nil || len(m) != 4 {
			return nil, fmt.Errorf("error parsing key %s", subkey)
		}
		v.User, v.Host, v.Port = m[1], m[2], m[3]
		conns[i] = v
	}

	return conns, nil
}

// FlatHost is a structure used to flatten a host informations present in etcd.
type FlatHost struct {
	Hostname string
	Port     string
	*Host
}

// GetAllHosts returns a list of all hosts present in etcd.
func (c *Client) GetAllHosts() ([]*FlatHost, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Get(ctx, etcdHostsPath, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	cancel()
	if err != nil {
		return nil, err
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
		hosts[i] = v
	}

	return hosts, nil
}

// IsAlive checks if etcd client is still usable.
func (c *Client) IsAlive() bool {
	return c.cli != nil
}
