package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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

// EtcdClient is a wrapper to easily do request to etcd cluster.
type EtcdClient struct {
	cli            *clientv3.Client
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

// NewEtcdClient creates a new etcd client.
func NewEtcdClient(config *sshProxyConfig) (*EtcdClient, error) {
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

	return &EtcdClient{
		cli:            cli,
		requestTimeout: 2 * time.Second,
	}, nil
}

// Close terminates the etcd client.
func (c *EtcdClient) Close() {
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
func (c *EtcdClient) Increment(key, dst string) error {
	for {
		if ok, err := c.doIncrement(key, dst); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
}

func (c *EtcdClient) doIncrement(key, dest string) (bool, error) {
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
func (c *EtcdClient) Decrement(key string) error {
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

func (c *EtcdClient) doDecrement(key string) (bool, error) {
	path := toConnectionKey(key)
	var conn Connection
	modRevision, err := c.get(path, &conn)
	if err != nil {
		if err == ErrKeyNotFound {
			log.Errorf("decrementing %s: key does not exist", key)
			return true, nil
		}
		return false, err
	}

	conn.N--

	if conn.N == 0 {
		log.Info("no more active connection for %s (to %s): removing", key, conn.Dest)
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
func (c *EtcdClient) get(key string, v interface{}) (int64, error) {
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
func (c *EtcdClient) GetDestination(key string) (string, error) {
	var conn Connection
	path := toConnectionKey(key)
	if _, err := c.get(path, &conn); err != nil {
		return "", err
	}
	return conn.Dest, nil
}

// GetHost returns the host (passed as "host:port") details. It host if not
// present the error will be etcd.ErrKeyNotFound.
func (c *EtcdClient) GetHost(hostport string) (*Host, error) {
	var h Host
	key := toHostKey(hostport)
	if _, err := c.get(key, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// SetHost sets a host (passed as "host:port") state and last checked time (ts)
// in etcd.
func (c *EtcdClient) SetHost(hostport string, state State, ts time.Time) error {
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

// IsAlive checks if etcd client is still usable.
func (c *EtcdClient) IsAlive() bool {
	return c.cli != nil
}
