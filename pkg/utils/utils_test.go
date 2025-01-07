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
	"reflect"
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

func BenchmarkCalcSessionID(b *testing.B) {
	d := time.Unix(1136239445, 0)
	for i := 0; i < b.N; i++ {
		CalcSessionID("arno", d, "127.0.0.1:22")
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

func mockNetLookupHost(host string) ([]string, error) {
	if host == "err" {
		return nil, errors.New("LookupHost error")
	} else if host == "127.0.0.1" {
		return []string{"127.0.0.1"}, nil
	}
	return []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}, nil
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
		b.Run(tt.source + "_" + tt.sshdHostport, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				MatchSource(tt.source, tt.sshdHostport)
			}
		})
	}
}
