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

// normalizeHostPort returns a slice of strings of IPs (hostnames are resolved)
// and a string containing the port (defaults to 22)
func normalizeHostPort(hostPort string) ([]string, string, error) {
	host, port, err := SplitHostPort(hostPort)
	if err != nil {
		return nil, "", fmt.Errorf("invalid address: %s", err)
	}
	var addrs []string
	if net.ParseIP(host) == nil {
		// host is a name and not an IP address
		// this name can resolve to multiple IPs
		addrs, err = netLookupHost(host)
		if err != nil {
			return nil, "", fmt.Errorf("cannot resolve host '%s': %v", host, err)
		}
	} else {
		addrs = []string{host}
	}
	return addrs, port, nil
}

// MatchSource checks if a source matches the SSH host and port (defaults to
// 22) of the incoming ssh connection
func MatchSource(source string, sshdHostPort string) (bool, error) {
	sourceAddrs, sourcePort, err := normalizeHostPort(source)
	if err != nil {
		return false, fmt.Errorf("source: %s", err)
	}
	sshdAddrs, sshdPort, err := normalizeHostPort(sshdHostPort)
	if err != nil {
		return false, fmt.Errorf("sshdHostPort: %s", err)
	}
	if sourcePort != sshdPort {
		return false, nil
	}
	for _, sourceAddr := range sourceAddrs {
		for _, sshdAddr := range sshdAddrs {
			if sourceAddr == sshdAddr {
				return true, nil
			}
		}
	}
	return false, nil
}
