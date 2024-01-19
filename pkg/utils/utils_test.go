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
	"errors"
	"fmt"
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

func mockNetLookupHost(host string) ([]string, error) {
	if host == "err" {
		return nil, errors.New("LookupHost error")
	}
	return []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}, nil
}

var checkroutesTests = []struct {
	routes, want map[string]*RouteConfig
}{
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "random",
			Mode:        "sticky",
			Source:      []string{"127.0.0.1:22"},
			Dest:        []string{"1.1.1.1"}}},
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "random",
			Mode:        "sticky",
			Source:      []string{"127.0.0.1:22"},
			Dest:        []string{"1.1.1.1:22"}}},
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "connections",
			Mode:        "balanced",
			Source:      []string{"127.0.0.1:22"},
			Dest:        []string{"host"}}},
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "connections",
			Mode:        "balanced",
			Source:      []string{"127.0.0.1:22"},
			Dest:        []string{"host:22"}}},
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "bandwidth",
			Source:      []string{"127.0.0.1:22"},
			Dest:        []string{"1.1.1.1:123"}}},
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "bandwidth",
			Mode:        "sticky",
			Source:      []string{"127.0.0.1:22"},
			Dest:        []string{"1.1.1.1:123"}}},
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "ordered",
			Source:      []string{"127.0.0.1"},
			Dest:        []string{"1.1.1.1"}}},
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "ordered",
			Mode:        "sticky",
			Source:      []string{"127.0.0.1:22"},
			Dest:        []string{"1.1.1.1:22"}}},
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			Source: []string{"host"},
			Dest:   []string{"1.1.1.1"}}},
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "ordered",
			Mode:        "sticky",
			Source:      []string{"1.1.1.1:22", "2.2.2.2:22", "3.3.3.3:22"},
			Dest:        []string{"1.1.1.1:22"}}},
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			Source: []string{"host:22"},
			Dest:   []string{"1.1.1.1"}}},
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "ordered",
			Mode:        "sticky",
			Source:      []string{"1.1.1.1:22", "2.2.2.2:22", "3.3.3.3:22"},
			Dest:        []string{"1.1.1.1:22"}}},
	},
	{
		map[string]*RouteConfig{"default": &RouteConfig{
			Dest: []string{"1.1.1.1"}}},
		map[string]*RouteConfig{"default": &RouteConfig{
			RouteSelect: "ordered",
			Mode:        "sticky",
			Dest:        []string{"1.1.1.1:22"}}},
	},
}

func TestCheckRoutes(t *testing.T) {
	netLookupHost = mockNetLookupHost
	for _, tt := range checkroutesTests {
		err := CheckRoutes(tt.routes)
		if err != nil {
			t.Errorf("%v CheckRoutes error = %v, want nil", tt.routes, err)
		} else if !reflect.DeepEqual(tt.routes, tt.want) {
			t.Errorf("CheckRoutes %s, want %s", displayRoutes(tt.routes), displayRoutes(tt.want))
		}
	}
}

var checkroutesInvalidTests = []struct {
	routes map[string]*RouteConfig
	want   string
}{
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			Source: []string{"host:port:invalid"},
			Dest:   []string{}}},
		"invalid source address: address host:port:invalid: too many colons in address",
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			Source: []string{"err"},
			Dest:   []string{}}},
		"cannot resolve host 'err': LookupHost error",
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			Source: []string{"host"},
			Dest:   []string{"host:port"}}},
		"invalid destination 'host:port' for service 'service1': address host:port: invalid port",
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			Dest: []string{"host"}}},
		"no source defined for service 'service1'",
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			Source: []string{"host"}}},
		"no destination defined for service 'service1'",
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			RouteSelect: "err",
			Source:      []string{"127.0.0.1"},
			Dest:        []string{"host"}}},
		"invalid value for `route_select` option of service 'service1': err",
	},
	{
		map[string]*RouteConfig{"service1": &RouteConfig{
			Mode:   "err",
			Source: []string{"127.0.0.1"},
			Dest:   []string{"host"}}},
		"invalid value for `mode` option of service 'service1': err",
	},
	{
		map[string]*RouteConfig{"default": &RouteConfig{
			Source: []string{"127.0.0.1"},
			Dest:   []string{"host"}}},
		"no source should be defined for the default service",
	},
}

func TestInvalidCheckRoutes(t *testing.T) {
	netLookupHost = mockNetLookupHost
	for _, tt := range checkroutesInvalidTests {
		err := CheckRoutes(tt.routes)
		if err == nil {
			t.Errorf("%v CheckRoutes got no error", tt.routes)
		} else if err.Error() != tt.want {
			t.Errorf("%v CheckRoutes error = %v, want %v", tt.routes, err, tt.want)
		}
	}
}

func displayRoutes(routes map[string]*RouteConfig) string {
	display := ""
	for service, route := range routes {
		display += fmt.Sprintf("%s: %v", service, route)
	}
	return display
}
