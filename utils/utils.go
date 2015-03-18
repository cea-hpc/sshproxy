package utils

import (
	"crypto/sha1"
	"fmt"
	"net"
	"time"
)

// CalcSessionId returns a unique 10 hexadecimal characters string from
// a user name, time, ip address and port.
func CalcSessionId(user string, t time.Time, ip net.IP, port int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s@%s:%d@%d", user, ip, port, t.UnixNano())))
	return fmt.Sprintf("%X", sum[:5])
}
