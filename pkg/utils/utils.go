// Copyright 2015-2022 CEA/DAM/DIF
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
	"crypto/sha1"
	"fmt"
	"net"
	"os/user"
	"strconv"
	"time"
)

// DefaultSSHPort is the default SSH server port.
const DefaultSSHPort = "22"

// DefaultService is the default service name.
const DefaultService = "default"

// CalcSessionID returns a unique 10 hexadecimal characters string from
// a user name, time, ip address and port.
func CalcSessionID(user string, t time.Time, hostport string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s@%s@%d", user, hostport, t.UnixNano())))
	return fmt.Sprintf("%X", sum[:5])
}

// SplitHostPort splits a network address of the form "host:port" or
// "host[:port]" into host and port. If the port is not specified the default
// ssh port ("22") is returned.
func SplitHostPort(hostport string) (string, string, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		if err.(*net.AddrError).Err == "missing port in address" {
			return hostport, DefaultSSHPort, nil
		}
		return hostport, DefaultSSHPort, err
	}
	portNum, err := net.LookupPort("tcp", port)
	if err != nil {
		return "", "", fmt.Errorf("address %s: invalid port", hostport)
	}
	return host, strconv.Itoa(portNum), nil
}

// GetGroupUser returns a map of group memberships for the specifised user.
//
// It can be used to quickly check if a user is in a specified group.
func GetGroupUser(u *user.User) (map[string]bool, error) {
	groupids, err := u.GroupIds()
	if err != nil {
		return nil, err
	}

	groups := make(map[string]bool)
	for _, gid := range groupids {
		g, err := user.LookupGroupId(gid)
		if err != nil {
			return nil, err
		}

		groups[g.Name] = true
	}

	return groups, nil
}

// GetGroups returns a map of group memberships for the current user.
//
// It can be used to quickly check if a user is in a specified group.
func GetGroups() (map[string]bool, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}

	groups, err := GetGroupUser(u)
	if err != nil {
		return nil, err
	}

	return groups, nil
}

// GetGroupList returns a map of group memberships for the specified user.
//
// It can be used to quickly check if a user is in a specified group.
func GetGroupList(username string) (map[string]bool, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}

	groups, err := GetGroupUser(u)
	if err != nil {
		return nil, err
	}

	return groups, nil
}

// Mocking net.LookupHost for testing.
var netLookupHost = net.LookupHost

// CheckRoutes checks and replaces all hosts defined in a map of routes with
// their "host:port" value (in case the host is defined without a port).
func CheckRoutes(routes map[string]*RouteConfig) error {
	for service, opts := range routes {
		if service != DefaultService && len(opts.Source) == 0 {
			return fmt.Errorf("no source defined for service '%s'", service)
		} else if service == DefaultService && len(opts.Source) != 0 {
			return fmt.Errorf("no source should be defined for the default service")
		}
		var sources []string
		for _, source := range opts.Source {
			host, port, err := SplitHostPort(source)
			if err != nil {
				return fmt.Errorf("invalid source address: %s", err)
			}
			var addrs []string
			if net.ParseIP(host) == nil {
				// host is a name and not an IP address
				// this name can resolve to multiple IPs
				addrs, err = netLookupHost(host)
				if err != nil {
					return fmt.Errorf("cannot resolve host '%s': %v", host, err)
				}
			} else {
				addrs = []string{host}
			}
			for _, addr := range addrs {
				hostport := net.JoinHostPort(addr, port)
				sources = append(sources, hostport)
			}
		}
		routes[service].Source = sources

		if len(opts.Dest) == 0 {
			return fmt.Errorf("no destination defined for service '%s'", service)
		}
		for i, dst := range opts.Dest {
			host, port, err := SplitHostPort(dst)
			if err != nil {
				return fmt.Errorf("invalid destination '%s' for service '%s': %s", dst, service, err)
			}
			routes[service].Dest[i] = net.JoinHostPort(host, port)
		}

		if opts.RouteSelect == "" {
			routes[service].RouteSelect = DefaultAlgorithm
		}
		if !IsRouteAlgorithm(routes[service].RouteSelect) {
			return fmt.Errorf("invalid value for `route_select` option of service '%s': %s", service, routes[service].RouteSelect)
		}

		if opts.Mode == "" {
			routes[service].Mode = DefaultMode
		}
		if !IsRouteMode(routes[service].Mode) {
			return fmt.Errorf("invalid value for `mode` option of service '%s': %s", service, routes[service].Mode)
		}
	}
	return nil
}
