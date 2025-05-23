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
	"math/rand"
	"net"
	"sort"
	"time"

	"github.com/op/go-logging"
)

var mylog = logging.MustGetLogger("sshproxy")

type selectDestinationFunc func([]string, HostChecker, *Client, string) (string, error)

// HostChecker is the interface that wraps the Check method.
//
// Check tests if a connection to host:port can be made.
type HostChecker interface {
	Check(hostport string) bool
}

var (
	routeSelecters = map[string]selectDestinationFunc{
		"ordered":     selectDestinationOrdered,
		"random":      selectDestinationRandom,
		"connections": selectDestinationConnections,
		"bandwidth":   selectDestinationBandwidth,
	}
)

// Mocking net.DialTimeout for testing.
var NetDialTimeout = net.DialTimeout

// CanConnect tests if a connection to host:port can be made (with a 1s timeout).
func CanConnect(hostport string) bool {
	c, err := NetDialTimeout("tcp", hostport, time.Second)
	if err != nil {
		mylog.Infof("cannot connect to %s: %s", hostport, err)
		return false
	}
	c.Close()
	return true
}

// selectDestinationOrdered selects the first reachable destination from a list
// of destinations. It returns a string "host:port", an empty string (if no
// destination is found) or an error.
func selectDestinationOrdered(destinations []string, checker HostChecker, cli *Client, key string) (string, error) {
	for _, dst := range destinations {
		if checker == nil || checker.Check(dst) {
			return dst, nil
		}
	}
	return "", nil
}

// selectDestinationRandom randomizes the order of the provided list of
// destinations and selects the first reachable one. It returns its host and
// port.
func selectDestinationRandom(destinations []string, checker HostChecker, cli *Client, key string) (string, error) {
	rdestinations := make([]string, len(destinations))
	perm := rand.Perm(len(destinations))
	for i, v := range perm {
		rdestinations[i] = destinations[v]
	}
	mylog.Debugf("randomized destinations: %v", rdestinations)
	return selectDestinationOrdered(rdestinations, checker, cli, key)
}

// selectDestinationConnections selects the destination you have less
// connection to. In case of a draw, it selects the one with the less overall
// connections. In case of a second draw, it randomizes the choice. It returns
// its host and port.
func selectDestinationConnections(destinations []string, checker HostChecker, cli *Client, key string) (string, error) {
	if cli != nil && cli.IsAlive() {
		userHosts, err := cli.GetUserHosts(key)
		if err != nil {
			return "", nil
		}
		userHostsc := map[string]int{}
		for _, userHost := range userHosts {
			userHostsc[userHost.Hostname] = userHost.N
		}
		hosts, err := cli.GetAllHosts()
		if err != nil {
			return "", nil
		}
		hostsc := map[string]int{}
		for _, host := range hosts {
			hostsc[host.Hostname] = host.N
		}
		sort.Slice(destinations, func(i, j int) bool {
			switch {
			case userHostsc[destinations[i]] != userHostsc[destinations[j]]:
				return userHostsc[destinations[i]] < userHostsc[destinations[j]]
			case hostsc[destinations[i]] != hostsc[destinations[j]]:
				return hostsc[destinations[i]] < hostsc[destinations[j]]
			default:
				return rand.Intn(2) != 0
			}
		})
		mylog.Debugf("ordered destinations based on # of connections: %v", destinations)
		return selectDestinationOrdered(destinations, checker, cli, key)
	}
	return selectDestinationRandom(destinations, checker, cli, key)
}

// selectDestinationBandwidth selects the destination you have less bandwidth
// used. In case of a draw, it selects the one with the less overall bandwidth
// used. In case of a second draw, it randomizes the choice. It returns its
// host and port.
func selectDestinationBandwidth(destinations []string, checker HostChecker, cli *Client, key string) (string, error) {
	if cli != nil && cli.IsAlive() {
		userHosts, err := cli.GetUserHosts(key)
		if err != nil {
			return "", nil
		}
		userHostsbw := map[string]uint64{}
		for _, userHost := range userHosts {
			userHostsbw[userHost.Hostname] = (uint64(userHost.BwIn) * uint64(userHost.BwIn)) + (uint64(userHost.BwOut) * uint64(userHost.BwOut)) + uint64(userHost.N)
		}
		hosts, err := cli.GetAllHosts()
		if err != nil {
			return "", nil
		}
		hostsbw := map[string]int{}
		for _, host := range hosts {
			hostsbw[host.Hostname] = (host.BwIn * host.BwIn) + (host.BwOut * host.BwOut) + host.N
		}
		sort.Slice(destinations, func(i, j int) bool {
			switch {
			case userHostsbw[destinations[i]] != userHostsbw[destinations[j]]:
				return userHostsbw[destinations[i]] < userHostsbw[destinations[j]]
			case hostsbw[destinations[i]] != hostsbw[destinations[j]]:
				return hostsbw[destinations[i]] < hostsbw[destinations[j]]
			default:
				return rand.Intn(2) != 0
			}
		})
		mylog.Debugf("ordered destinations based on bandwidth used: %v", destinations)
		return selectDestinationOrdered(destinations, checker, cli, key)
	}
	return selectDestinationRandom(destinations, checker, cli, key)
}

// SelectRoute returns a destination among the destinations according to the
// specified algo. The destination was successfully checked by the specified
// checker.
func SelectRoute(algo string, destinations []string, checker HostChecker, cli *Client, key string) (string, error) {
	return routeSelecters[algo](destinations, checker, cli, key)
}

// IsRouteAlgorithm checks if the specified algo is valid.
func IsRouteAlgorithm(algo string) bool {
	_, ok := routeSelecters[algo]
	return ok
}

// IsRouteMode checks if the specified mode is valid.
func IsRouteMode(mode string) bool {
	for _, realMode := range []string{"sticky", "balanced"} {
		if mode == realMode {
			return true
		}
	}
	return false
}
