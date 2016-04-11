// Copyright 2015 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package route

import (
	"math/rand"
	"net"
	"time"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("sshproxy/route")

type selectDestinationFunc func([]string, HostChecker) (string, error)

var (
	// default algorithm to find route
	DefaultAlgorithm = "ordered"
	// keyword for default route
	DefaultRouteKeyword = "default:22"
)

// HostChecker is the interface that wraps the Check method.
//
// Check tests if a connection to host:port can be made.
type HostChecker interface {
	Check(hostport string) bool
}

// BasicHostChecker implements the HostChecker interface.
type BasicHostChecker struct{}

// Check tests if a conneciton to host:port can be made with a 1s timeout.
func (bhc *BasicHostChecker) Check(hostport string) bool {
	return CanConnect(hostport)
}

var (
	routeSelecters = map[string]selectDestinationFunc{
		"ordered": selectDestinationOrdered,
		"random":  selectDestinationRandom,
	}
)

// CanConnect tests if a connection to host:port can be made (with a 1s timeout).
func CanConnect(hostport string) bool {
	c, err := net.DialTimeout("tcp", hostport, 1*time.Second)
	if err != nil {
		log.Info("cannot connect to %s: %s", hostport, err)
		return false
	}
	c.Close()
	return true
}

// selectDestinationOrdered selects the first reachable destination from a list
// of destinations. It returns a string "host:port", an empty string (if no
// destination is found) or an error.
func selectDestinationOrdered(destinations []string, checker HostChecker) (string, error) {
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
func selectDestinationRandom(destinations []string, checker HostChecker) (string, error) {
	rand.Seed(time.Now().UnixNano())
	rdestinations := make([]string, len(destinations))
	perm := rand.Perm(len(destinations))
	for i, v := range perm {
		rdestinations[i] = destinations[v]
	}
	log.Debug("randomized destinations: %v", rdestinations)
	return selectDestinationOrdered(rdestinations, checker)
}

func Select(route_select string, destinations []string, checker HostChecker) (string, error) {
	return routeSelecters[route_select](destinations, checker)
}

func IsAlgorithm(algo string) bool {
	_, ok := routeSelecters[algo]
	return ok
}
