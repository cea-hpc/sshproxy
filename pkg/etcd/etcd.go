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
	keyTTL         int64
}

// Host represent the state of a host.
type Host struct {
	State State     // host state (see State const for available states)
	Ts    time.Time // time of last check
}

// NewClient creates a new etcd client.
func NewClient(config *utils.Config, log *logging.Logger) (*Client, error) {
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
func (c *Client) SetDestination(rootctx context.Context, key, dst string) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	path := fmt.Sprintf("%s/%s/%s", toConnectionKey(key), dst, time.Now().Format(time.RFC3339Nano))
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	resp, err := c.cli.Grant(ctx, c.keyTTL)
	cancel()
	if err != nil {
		return nil, err
	}

	ctx, cancel = context.WithTimeout(context.Background(), c.requestTimeout)
	_, err = c.cli.Put(ctx, path, "1", clientv3.WithLease(resp.ID))
	cancel()
	if err != nil {
		return nil, err
	}

	return c.cli.KeepAlive(rootctx, resp.ID)
}

// GetHost returns the host (passed as "host:port") details. It host if not
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
	Dest string
	Ts   time.Time
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
		if len(fields) != 3 {
			return nil, fmt.Errorf("bad key format %s", subkey)
		}

		userhostport := fields[0]
		v.Dest = fields[1]
		v.Ts, err = time.Parse(time.RFC3339Nano, fields[2])
		if err != nil {
			return nil, fmt.Errorf("error parsing time %s", fields[2])
		}

		m := keyRegex.FindStringSubmatch(userhostport)
		if m == nil || len(m) != 4 {
			return nil, fmt.Errorf("error parsing key %s", userhostport)
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
