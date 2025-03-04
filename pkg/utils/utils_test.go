// Copyright 2015-2025 CEA/DAM/DIF
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
	"errors"
	"fmt"
	"os/user"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCalcSessionID(t *testing.T) {
	want := "C028E7684F"
	d := time.Unix(1136239445, 0)
	if got := CalcSessionID("arno", d, "127.0.0.1:22"); got != want {
		t.Errorf("session id = %q, want = %q", got, want)
	}
}

var calcSessionIDBenchmarks = []struct {
	username, hostport string
}{
	{"arno", "127.0.0.1:22"},
	{"arno", "192.168.100.100:1234"},
	{"cyril", "127.0.0.1:22"},
	{"cyril", "192.168.100.100:1234"},
}

func BenchmarkCalcSessionID(b *testing.B) {
	d := time.Now()
	for _, tt := range calcSessionIDBenchmarks {
		b.Run(tt.username+"_"+tt.hostport, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				CalcSessionID(tt.username, d, tt.hostport)
			}
		})
	}
}

var splithostportTests = []struct {
	hostport, host, port string
}{
	{"host", "host", DefaultSSHPort},
	{"host:123", "host", "123"},
	{"host:gopher", "host", "70"},
	{"[::1]", "[::1]", DefaultSSHPort},
	{"[::1]:123", "::1", "123"},
	{"[::1]:gopher", "::1", "70"},
}

func TestSplitHostPort(t *testing.T) {
	for _, tt := range splithostportTests {
		host, port, err := SplitHostPort(tt.hostport)
		if err != nil {
			t.Errorf("%v SplitHostPort error = %v, want nil", tt.hostport, err)
		} else if host != tt.host {
			t.Errorf("%v SplitHostPort host = %v, want %v", tt.hostport, host, tt.host)
		} else if port != tt.port {
			t.Errorf("%v SplitHostPort port = %v, want %v", tt.hostport, port, tt.port)
		}
	}
}

var splithostportInvalidTests = []struct {
	hostport, want string
}{
	{"host:port:invalid", "address host:port:invalid: too many colons in address"},
	{"host:port", "address host:port: invalid port"},
}

func TestInvalidSplitHostPort(t *testing.T) {
	for _, tt := range splithostportInvalidTests {
		_, _, err := SplitHostPort(tt.hostport)
		if err == nil {
			t.Errorf("%v SplitHostPort got no error", tt.hostport)
		} else if err.Error() != tt.want {
			t.Errorf("%v SplitHostPort error = %v, want %v", tt.hostport, err, tt.want)
		}
	}
}

func BenchmarkSplitHostPort(b *testing.B) {
	for _, tt := range splithostportTests {
		b.Run(tt.hostport, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				SplitHostPort(tt.hostport)
			}
		})
	}
}

func mockUserCurrent() (*user.User, error) {
	return mockUserLookup("testuser")
}

func mockUserLookup(username string) (*user.User, error) {
	var u user.User
	if username == "root" {
		u.Uid = "0"
		u.Gid = "0"
	} else if username == "testuser" {
		u.Uid = "1000"
		u.Gid = "1000"
	} else if username == "userwithnogroupid" {
		u.Uid = "1001"
	} else if username == "userwithinvalidgroup" {
		u.Uid = "1002"
		u.Gid = "1002"
	} else {
		return &u, fmt.Errorf("user: unknown user %s", username)
	}
	u.Username = username
	return &u, nil
}

func mockUserLookupGroupId(gid string) (*user.Group, error) {
	var g user.Group
	g.Gid = gid
	if gid == "0" {
		g.Name = "root"
	} else if gid == "1000" {
		g.Name = "testgroup"
	} else {
		return &g, fmt.Errorf("group: unknown group ID %s", gid)
	}
	return &g, nil
}

func BenchmarkGetGroupUser(b *testing.B) {
	current, _ := userCurrent()
	root, _ := userLookup("root")
	for _, tt := range []*user.User{current, root} {
		b.Run(tt.Username, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				GetGroupUser(tt)
			}
		})
	}
}

func TestGetGroups(t *testing.T) {
	userCurrent = mockUserCurrent
	userLookupGroupId = mockUserLookupGroupId
	groups, err := GetGroups()
	if err != nil {
		t.Errorf("GetGroups error = '%v', want nil", err)
	} else if len(groups) < 1 {
		t.Error("GetGroups must return at least one group")
	}
}

func BenchmarkGetGroups(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetGroups()
	}
}

var getGroupListTests = []struct {
	user, groups, err string
}{
	{"root", "root", ""},
	{"testuser", "testgroup", ""},
	{"userwithnogroupid", "", "user: list groups for userwithnogroupid: invalid gid \"\""},
	{"userwithinvalidgroup", "nonexistentgroup", "group: unknown group ID 1002"},
	{"nonexistentuser", "nonexistentgroup", "user: unknown user nonexistentuser"},
}

func TestGetGroupList(t *testing.T) {
	userLookup = mockUserLookup
	userLookupGroupId = mockUserLookupGroupId
	for _, tt := range getGroupListTests {
		groups, err := GetGroupList(tt.user)
		if err != nil {
			if fmt.Sprintf("%s", err) != tt.err {
				t.Errorf("GetGroupList error = '%v', want '%v'", err, tt.err)
			}
		} else {
			g := make([]string, 0, len(groups))
			for group := range groups {
				g = append(g, group)
			}
			sort.Strings(g)
			if strings.Join(g, " ") != tt.groups {
				t.Errorf("GetGroupList groups = %v, want %v", strings.Join(g, " "), tt.groups)
			}
		}
	}
}

func BenchmarkGetGroupList(b *testing.B) {
	for _, tt := range getGroupListTests {
		if tt.err == "" {
			b.Run(tt.user, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					GetGroupList(tt.user)
				}
			})
		}
	}
}

func mockNetLookupHost(host string) ([]string, error) {
	if host == "err" {
		return nil, errors.New("LookupHost error")
	} else if host == "127.0.0.1" {
		return []string{"127.0.0.1"}, nil
	}
	return []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}, nil
}

func BenchmarkNormalizeHostPort(b *testing.B) {
	netLookupHost = mockNetLookupHost
	for _, tt := range []string{"127.0.0.1", "127.0.0.1:123", "server1", "server1:123", "host:port:invalid", "err"} {
		b.Run(tt, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				normalizeHostPort(tt)
			}
		})
	}
}

var matchSourceTests = []struct {
	source, sshdHostport string
	want                 bool
}{
	{
		"127.0.0.1",
		"127.0.0.1:22",
		true,
	},
	{
		"127.0.0.1:22",
		"127.0.0.1:22",
		true,
	},
	{
		"127.0.0.1:22",
		"127.0.0.1",
		true,
	},
	{
		"127.0.0.1:123",
		"127.0.0.1:123",
		true,
	},
	{
		"server1",
		"1.1.1.1:22",
		true,
	},
	{
		"server1:22",
		"1.1.1.1:22",
		true,
	},
	{
		"server1:123",
		"1.1.1.1:123",
		true,
	},
	{
		"server1",
		"2.2.2.2:22",
		true,
	},
	{
		"1.1.1.1",
		"server1",
		true,
	},
	{
		"server1",
		"server2",
		true,
	},
	{
		"127.0.0.1:22",
		"127.0.0.1:122",
		false,
	},
	{
		"127.0.0.1:22",
		"127.0.0.2:22",
		false,
	},
}

func TestMatchSource(t *testing.T) {
	netLookupHost = mockNetLookupHost
	for _, tt := range matchSourceTests {
		match, err := MatchSource(tt.source, tt.sshdHostport)
		if err != nil {
			t.Errorf("MatchSource(%s, %s) error = %v, want nil", tt.source, tt.sshdHostport, err)
		} else if !reflect.DeepEqual(match, tt.want) {
			t.Errorf("MatchSource(%s, %s) %v, want %v", tt.source, tt.sshdHostport, match, tt.want)
		}
	}
}

var matchSourceInvalidTests = []struct {
	source, sshdHostport, want string
}{
	{
		"host:port:invalid",
		"host",
		"source: invalid address: address host:port:invalid: too many colons in address",
	},
	{
		"host:port",
		"host",
		"source: invalid address: address host:port: invalid port",
	},
	{
		"err",
		"host",
		"source: cannot resolve host 'err': LookupHost error",
	},
	{
		"host",
		"host:port:invalid",
		"sshdHostPort: invalid address: address host:port:invalid: too many colons in address",
	},
	{
		"host",
		"host:port",
		"sshdHostPort: invalid address: address host:port: invalid port",
	},
	{
		"host",
		"err",
		"sshdHostPort: cannot resolve host 'err': LookupHost error",
	},
}

func TestInvalidMatchSource(t *testing.T) {
	netLookupHost = mockNetLookupHost
	for _, tt := range matchSourceInvalidTests {
		_, err := MatchSource(tt.source, tt.sshdHostport)
		if err == nil {
			t.Errorf("MatchSource(%s, %s) got no error", tt.source, tt.sshdHostport)
		} else if err.Error() != tt.want {
			t.Errorf("MatchSource(%s, %s) error = %v, want %v", tt.source, tt.sshdHostport, err, tt.want)
		}
	}
}

func BenchmarkMatchSource(b *testing.B) {
	netLookupHost = mockNetLookupHost
	for _, tt := range matchSourceTests {
		b.Run(tt.source+"_"+tt.sshdHostport, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				MatchSource(tt.source, tt.sshdHostport)
			}
		})
	}
}
