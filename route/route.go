package route

import (
	"fmt"
	"math/rand"
	"net"
	"time"

	"sshproxy/utils"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("sshproxy/route")

type chooseDestinationFunc func([]string, bool) (string, string, error)

// default algorithm to find route
var DefaultAlgorithm = "ordered"

var (
	routeChoosers = map[string]chooseDestinationFunc{
		"ordered": chooseDestinationOrdered,
		"random":  chooseDestinationRandom,
	}
)

// CanConnect tests if a connection to host:port can be made (with a 1s timeout).
func CanConnect(host, port string) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1*time.Second)
	if err != nil {
		log.Info("cannot connect to %s:%s: %s", host, port, err)
		return false
	}
	c.Close()
	return true
}

// chooseDestinationOrdered chooses the first reachable destination from a list
// of destinations. It returns its host and port.
func chooseDestinationOrdered(destinations []string, check_host bool) (string, string, error) {
	for i, dst := range destinations {
		host, port, err := utils.SplitHostPort(dst)
		if err != nil {
			return "", "", err
		}

		// always return the last destination without trying to connect
		if i == len(destinations)-1 {
			return host, port, nil
		}
		if !check_host || CanConnect(host, port) {
			return host, port, nil
		}
	}
	return "", "", fmt.Errorf("no valid destination found")
}

// chooseDestinationRandom randomizes the order of the provided list of
// destinations and chooses the first reachable one. It returns its host and
// port.
func chooseDestinationRandom(destinations []string, check_host bool) (string, string, error) {
	rand.Seed(time.Now().UnixNano())
	rdestinations := make([]string, len(destinations))
	perm := rand.Perm(len(destinations))
	for i, v := range perm {
		rdestinations[i] = destinations[v]
	}
	log.Debug("randomized destinations: %v", rdestinations)
	return chooseDestinationOrdered(rdestinations, check_host)
}

func Chose(route_choice string, destinations []string, check_host bool) (string, string, error) {
	return routeChoosers[route_choice](destinations, check_host)
}

func IsAlgorithm(algo string) bool {
	_, ok := routeChoosers[algo]
	return ok
}
