package utils

import (
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
