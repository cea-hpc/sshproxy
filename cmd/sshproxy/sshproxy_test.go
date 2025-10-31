// Copyright 2015-2025 CEA/DAM/DIF
//  Author: Arnaud Guignard <arnaud.guignard@cea.fr>
//  Contributor: Cyril Servant <cyril.servant@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/op/go-logging"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/client/v3"

	"github.com/cea-hpc/sshproxy/pkg/utils"
)

func setTestLogBackend() *logging.MemoryBackend {
	backend := logging.NewMemoryBackend(16)
	logging.SetBackend(backend)
	log = logging.MustGetLogger("test")
	return backend
}

func getTestLogs(backend *logging.MemoryBackend) []string {
	node := backend.Head()
	logs := []string{}
	for {
		if node == nil {
			break
		}
		if node.Record.Level != logging.DEBUG {
			logs = append(logs, node.Record.Message())
		}
		node = node.Next()
	}
	return logs
}

// Mock net.Conn and all its methods
type mockNetConn struct{}

func (m *mockNetConn) Read(b []byte) (n int, err error) {
	return 0, nil
}

func (m *mockNetConn) Write(b []byte) (n int, err error) {
	return 0, nil
}

func (m *mockNetConn) Close() error {
	return nil
}

func (m *mockNetConn) LocalAddr() net.Addr {
	return nil
}

func (m *mockNetConn) RemoteAddr() net.Addr {
	return nil
}

func (m *mockNetConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockNetConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockNetConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// Mock net.DialTimeout, which will return the mocked net.Conn
func mockNetDialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	m := mockNetConn{}
	if address == "downhost:22" || address == "errorhost:22" {
		return &m, fmt.Errorf("This is a wanted error, made up for the tests")
	}
	return &m, nil
}

// Mock clientv3.New, and especially it's mockClientv3kV
func mockClientv3New(cfg clientv3.Config) (*clientv3.Client, error) {
	c := clientv3.Client{
		KV: mockClientv3KV{},
	}
	return &c, nil
}

// Mock clientv3.VK and all its methods
type mockClientv3KV struct{}

func (m mockClientv3KV) Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
	if key == "/sshproxy/hosts/downhost:22" {
		return nil, fmt.Errorf("This is a wanted error, made up for the tests")
	}
	return nil, nil
}

func (m mockClientv3KV) Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	resp := clientv3.GetResponse{}
	host4, _ := json.Marshal(&utils.Host{
		State: utils.Up,
		Ts:    time.Now(),
	})
	switch key {
	case "/sshproxy/hosts":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte("/sshproxy/hosts/host1:22"),
					Value: []byte("{\"State\":\"up\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				}, {
					Key:   []byte("/sshproxy/hosts/host2:22"),
					Value: []byte("{\"State\":\"up\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				}, {
					Key:   []byte("/sshproxy/hosts/host3:22"),
					Value: []byte("{\"State\":\"up\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				}, {
					Key:   []byte("/sshproxy/hosts/host4:22"),
					Value: []byte(string(host4)),
				}, {
					Key:   []byte("/sshproxy/hosts/downhost:22"),
					Value: []byte("{\"State\":\"down\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				}, {
					Key:   []byte("/sshproxy/hosts/disabledhost:22"),
					Value: []byte("{\"State\":\"disabled\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				},
			},
		}
	case "/sshproxy/hosts/host1:22":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte(key),
					Value: []byte("{\"State\":\"up\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				},
			},
		}
	case "/sshproxy/hosts/host2:22":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte(key),
					Value: []byte("{\"State\":\"up\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				},
			},
		}
	case "/sshproxy/hosts/host3:22":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte(key),
					Value: []byte("{\"State\":\"up\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				},
			},
		}
	case "/sshproxy/hosts/host4:22":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte(key),
					Value: []byte(string(host4)),
				},
			},
		}
	case "/sshproxy/hosts/downhost:22":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte(key),
					Value: []byte("{\"State\":\"down\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				},
			},
		}
	case "/sshproxy/hosts/disabledhost:22":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte(key),
					Value: []byte("{\"State\":\"disabled\",\"Ts\":\"1970-01-01T15:14:47.949087374+01:00\"}"),
				},
			},
		}
	case "/sshproxy/hosts/errorhost:22":
		fallthrough
	case "/sshproxy/connections/arno@default":
		return &resp, fmt.Errorf("This is a wanted error, made up for the tests")
	case "/sshproxy/connections":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte("/sshproxy/connections/cyril@default/host2:22/source:22/1970-01-01T14:39:02.243768889+01:00"),
					Value: []byte("{\"In\":10,\"Out\":10}"),
				}, {
					Key:   []byte("/sshproxy/connections/bob@default/downhost:22/source:22/1970-01-01T14:39:02.243768889+01:00"),
					Value: []byte("{\"In\":10,\"Out\":10}"),
				},
			},
		}
	case "/sshproxy/connections/cyril@default":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte("/sshproxy/connections/cyril@default/host2:22/source:22/1970-01-01T14:39:02.243768889+01:00"),
					Value: []byte("{\"In\":10,\"Out\":10}"),
				},
			},
		}
	case "/sshproxy/connections/bob@default":
		resp = clientv3.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte("/sshproxy/connections/bob@default/downhost:22/source:22/1970-01-01T14:39:02.243768889+01:00"),
					Value: []byte("{\"In\":10,\"Out\":10}"),
				},
			},
		}
	}
	return &resp, nil
}

func (m mockClientv3KV) Delete(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
	return nil, nil
}

func (m mockClientv3KV) Compact(ctx context.Context, rev int64, opts ...clientv3.CompactOption) (*clientv3.CompactResponse, error) {
	return nil, nil
}

func (m mockClientv3KV) Do(ctx context.Context, op clientv3.Op) (clientv3.OpResponse, error) {
	return clientv3.OpResponse{}, nil
}

func (m mockClientv3KV) Txn(ctx context.Context) clientv3.Txn {
	return nil
}

var selectDestinationBandwidthTests = []struct {
	username string
	etcd     bool
	config   *utils.Config
	want     []string
	err      string
	logs     []string
}{
	{
		// simple test without etcd
		"cyril",
		false,
		&utils.Config{
			Dest:        []string{"host2:22", "host3:22"},
			RouteSelect: "bandwidth",
		},
		[]string{"host2:22", "host3:22"},
		"",
		[]string{},
	}, {
		// test without etcd to a down host
		"cyril",
		false,
		&utils.Config{
			Dest:        []string{"downhost:22"},
			RouteSelect: "ordered",
		},
		[]string{},
		"",
		[]string{"cannot connect to downhost:22: This is a wanted error, made up for the tests"},
	}, {
		// test without etcd and without any default destination
		"cyril",
		false,
		&utils.Config{
			RouteSelect: "ordered",
			Service:     "default",
		},
		[]string{},
		"no destination set for service default",
		[]string{},
	}, {
		// host2 can't be chosen because it has more active connections
		"alice",
		true,
		&utils.Config{
			Dest:        []string{"host2:22", "host3:22"},
			Service:     "default",
			RouteSelect: "connections",
		},
		[]string{"host3:22"},
		"",
		[]string{},
	}, {
		// host2 must be chosen because cyril already has an active connection to it
		"cyril",
		true,
		&utils.Config{
			Dest:        []string{"host3:22", "host4:22", "host2:22"},
			Service:     "default",
			RouteSelect: "ordered",
			Mode:        "sticky",
		},
		[]string{"host2:22"},
		"",
		[]string{},
	}, {
		// host4 will be chosen because the 3 first hosts have problems
		// those problems will be logged
		"alice",
		true,
		&utils.Config{
			Dest:        []string{"errorhost:22", "disabledhost:22", "downhost:22", "host4:22", "host2:22"},
			Service:     "default",
			RouteSelect: "ordered",
		},
		[]string{"host4:22"},
		"",
		[]string{
			"problem with etcd: This is a wanted error, made up for the tests",
			"cannot connect to errorhost:22: This is a wanted error, made up for the tests",
			"cannot connect to downhost:22: This is a wanted error, made up for the tests",
			"setting host state in etcd: This is a wanted error, made up for the tests",
		},
	}, {
		// there will be a log because cyril already has an active connection to
		// host2, but host2 is no longer in the config
		"cyril",
		true,
		&utils.Config{
			Dest:        []string{"host3:22"},
			Service:     "default",
			RouteSelect: "ordered",
			Mode:        "sticky",
		},
		[]string{"host3:22"},
		"",
		[]string{"cannot connect cyril@default to already existing connection(s) to host2:22: not in routes"},
	}, {
		// simulating simulaneous problems writing to etcd, and connecting to downhost
		"bob",
		true,
		&utils.Config{
			Dest:        []string{"downhost:22", "host3:22"},
			Service:     "default",
			RouteSelect: "ordered",
			Mode:        "sticky",
		},
		[]string{"host3:22"},
		"",
		[]string{
			"cannot connect to downhost:22: This is a wanted error, made up for the tests",
			"setting host state in etcd: This is a wanted error, made up for the tests",
			"cannot connect bob@default to already existing connection(s) to downhost:22: host down",
			"cannot connect to downhost:22: This is a wanted error, made up for the tests",
			"setting host state in etcd: This is a wanted error, made up for the tests",
		},
	}, {
		// simulating problem when reading from etcd
		"arno",
		true,
		&utils.Config{
			Dest:        []string{"host3:22"},
			Service:     "default",
			RouteSelect: "ordered",
			Mode:        "sticky",
		},
		[]string{"host3:22"},
		"",
		[]string{"problem with etcd: This is a wanted error, made up for the tests"},
	},
}

func TestFindDestination(t *testing.T) {
	utils.NetDialTimeout = mockNetDialTimeout
	utils.Clientv3New = mockClientv3New
	for _, tt := range selectDestinationBandwidthTests {
		logBackend := setTestLogBackend()
		cli, _ := utils.NewEtcdClient(tt.config, nil)
		if !tt.etcd {
			cli = nil
		}
		got, err := findDestination(cli, tt.username, tt.config, "source:22")
		if err == nil && tt.err != "" {
			t.Errorf("got no error, want %s", tt.err)
		} else if err != nil && err.Error() != tt.err {
			t.Errorf("ERROR: %s, want %s", err, tt.err)
		} else if err == nil && (len(tt.want) != 0 && got != "") && !slices.Contains(tt.want, got) {
			t.Errorf("want one of %v, got %s", tt.want, got)
		} else {
			logs := getTestLogs(logBackend)
			if !reflect.DeepEqual(tt.logs, logs) {
				t.Errorf("want %v, got %v", tt.logs, logs)
			}
		}
	}
}

func BenchmarkFindDestination(b *testing.B) {
	utils.NetDialTimeout = mockNetDialTimeout
	utils.Clientv3New = mockClientv3New
	for n, tt := range selectDestinationBandwidthTests {
		setTestLogBackend()
		cli, _ := utils.NewEtcdClient(tt.config, nil)
		if !tt.etcd {
			cli = nil
		}
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				findDestination(cli, tt.username, tt.config, "source:22")
			}
		})
	}
}

func TestSetEnvironment(t *testing.T) {
	key := "TEST_ENV"
	value := "testValue"
	os.Setenv(key, "")
	setEnvironment(map[string]string{key: value})
	got := os.Getenv(key)
	if got != value {
		t.Errorf("want %s, got %s", value, got)
	}
}

func BenchmarkSetEnvironment(b *testing.B) {
	key := "TEST_ENV"
	value := "testValue"
	os.Setenv(key, "")
	for i := 0; i < b.N; i++ {
		setEnvironment(map[string]string{key: value})
	}
}

var newSSHInfoTest = []struct {
	s    string
	want *SSHInfo
	err  string
}{
	{
		"127.0.0.1 1234 192.168.0.1 22",
		&SSHInfo{
			SrcIP:   net.ParseIP("127.0.0.1"),
			SrcPort: 1234,
			DstIP:   net.ParseIP("192.168.0.1"),
			DstPort: 22,
		},
		"",
	}, {
		"notAnIP 1234 192.168.0.1 22",
		&SSHInfo{},
		"bad value",
	}, {
		"1234 1234 192.168.0.1 22",
		&SSHInfo{},
		"bad value for source IP",
	}, {
		"127.0.0.1 1234 1234 22",
		&SSHInfo{},
		"bad value for destination IP",
	},
}

func TestNewSSHInfo(t *testing.T) {
	for _, tt := range newSSHInfoTest {
		got, err := NewSSHInfo(tt.s)
		if err == nil && tt.err != "" {
			t.Errorf("got no error, want %s", tt.err)
		} else if err != nil && err.Error() != tt.err {
			t.Errorf("ERROR: %s, want %s", err, tt.err)
		} else if err == nil && !reflect.DeepEqual(got, tt.want) {
			t.Errorf("want %v, got %v", tt.want, got)
		}
	}
}

func BenchmarkNewSSHInfo(b *testing.B) {
	for _, tt := range newSSHInfoTest {
		b.Run(tt.s, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				NewSSHInfo(tt.s)
			}
		})
	}
}

var srcDstTests = &SSHInfo{
	SrcIP:   net.ParseIP("127.0.0.1"),
	SrcPort: 1234,
	DstIP:   net.ParseIP("192.168.0.1"),
	DstPort: 22,
}

func TestSrcDst(t *testing.T) {
	want := "127.0.0.1:1234"
	got := srcDstTests.Src()
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
	want = "192.168.0.1:22"
	got = srcDstTests.Dst()
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func BenchmarkSrcDst(b *testing.B) {
	b.Run("src", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			srcDstTests.Src()
		}
	})
	b.Run("dst", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			srcDstTests.Dst()
		}
	})
}

var getOriginalCommandTests = []struct {
	sshUserAuth string
	wantCmd     string
	wantComment string
	logs        []string
}{
	{
		// without SSH_USER_AUTH
		"",
		"",
		" ",
		[]string{},
	}, {
		// with a wrong SSH_USER_AUTH
		"_non_existing_file_",
		"",
		" ",
		[]string{
			"open _non_existing_file_: no such file or directory",
		},
	}, {
		// SSH_USER_AUTH is not a publickey
		"../../test/sshUserAuthPassword",
		"",
		" ",
		[]string{},
	}, {
		// error in the publickey
		"../../test/sshUserAuthInvalidKey",
		"",
		" ",
		[]string{
			"ssh: no key found",
		},
	}, {
		// publickey is not a certificate
		"../../test/sshUserAuthKey",
		"",
		" ",
		[]string{},
	}, {
		// publickey is a certificate without force-command
		"../../test/sshUserAuthCert",
		"",
		" ",
		[]string{},
	}, {
		// publickey is a certificate with force-command
		"../../test/sshUserAuthCertForceCmd",
		"test-command",
		" (forced) ",
		[]string{},
	},
}

func TestGetOriginalCommand(t *testing.T) {
	for _, tt := range getOriginalCommandTests {
		logBackend := setTestLogBackend()
		os.Setenv("SSH_USER_AUTH", tt.sshUserAuth)
		originalCmd, comment := getOriginalCommand()
		if originalCmd != tt.wantCmd || comment != tt.wantComment {
			t.Errorf("want '%s' - '%s', got '%s' - '%s'", tt.wantCmd, tt.wantComment, originalCmd, comment)
		}
		logs := getTestLogs(logBackend)
		if !reflect.DeepEqual(tt.logs, logs) {
			t.Errorf("want %v, got %v", tt.logs, logs)
		}
	}
	os.Unsetenv("SSH_USER_AUTH")
}

func BenchmarkGetOriginalCommand(b *testing.B) {
	for _, tt := range getOriginalCommandTests {
		os.Setenv("SSH_USER_AUTH", tt.sshUserAuth)
		b.Run(tt.sshUserAuth, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				getOriginalCommand()
			}
		})
	}
	os.Unsetenv("SSH_USER_AUTH")
}
