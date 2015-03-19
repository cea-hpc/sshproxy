package utils

import (
	"crypto/sha1"
	"fmt"
	"net"
	"os"
	"time"

	"sshproxy/group.go"
)

const DefaultSshPort = "22"

// CalcSessionId returns a unique 10 hexadecimal characters string from
// a user name, time, ip address and port.
func CalcSessionId(user string, t time.Time, ip net.IP, port int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s@%s:%d@%d", user, ip, port, t.UnixNano())))
	return fmt.Sprintf("%X", sum[:5])
}

// SplitHostPort splits a network address of the form "host:port" or
// "host[:port]" into host and port. If the port is not specified the default
// ssh port ("22") is returned.
func SplitHostPort(hostport string) (string, string, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		if err.(*net.AddrError).Err == "missing port in address" {
			return hostport, DefaultSshPort, nil
		} else {
			return hostport, DefaultSshPort, err
		}
	}
	return host, port, nil
}

// GetGroups returns a map of group memberships for the current user.
//
// It can be used to quickly check if a user is in a specified group.
func GetGroups() (map[string]bool, error) {
	gids, err := os.Getgroups()
	if err != nil {
		return nil, err
	}

	groups := make(map[string]bool)
	for _, gid := range gids {
		g, err := group.LookupId(gid)
		if err != nil {
			return nil, err
		}

		groups[g.Name] = true
	}

	return groups, nil
}
