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
	routes, want map[string][]string
}{
	{
		map[string][]string{"127.0.0.1:22": []string{"1.1.1.1"}},
		map[string][]string{"127.0.0.1:22": []string{"1.1.1.1:22"}},
	},
	{
		map[string][]string{"127.0.0.1:22": []string{"host"}},
		map[string][]string{"127.0.0.1:22": []string{"host:22"}},
	},
	{
		map[string][]string{"127.0.0.1:22": []string{"1.1.1.1:123"}},
		map[string][]string{"127.0.0.1:22": []string{"1.1.1.1:123"}},
	},
	{
		map[string][]string{"127.0.0.1": []string{"1.1.1.1"}},
		map[string][]string{"127.0.0.1:22": []string{"1.1.1.1:22"}},
	},
	{
		map[string][]string{"default": []string{"1.1.1.1"}},
		map[string][]string{"default:22": []string{"1.1.1.1:22"}},
	},
	{
		map[string][]string{"host": []string{"1.1.1.1"}},
		map[string][]string{
			"1.1.1.1:22": []string{"1.1.1.1:22"},
			"2.2.2.2:22": []string{"1.1.1.1:22"},
			"3.3.3.3:22": []string{"1.1.1.1:22"}},
	},
	{
		map[string][]string{"host:22": []string{"1.1.1.1"}},
		map[string][]string{
			"1.1.1.1:22": []string{"1.1.1.1:22"},
			"2.2.2.2:22": []string{"1.1.1.1:22"},
			"3.3.3.3:22": []string{"1.1.1.1:22"}},
	},
}

func TestCheckRoutes(t *testing.T) {
	netLookupHost = mockNetLookupHost
	for _, tt := range checkroutesTests {
		err := CheckRoutes(tt.routes)
		if err != nil {
			t.Errorf("%v CheckRoutes error = %v, want nil", tt.routes, err)
		} else if !reflect.DeepEqual(tt.routes, tt.want) {
			t.Errorf("CheckRoutes %v, want %v", tt.routes, tt.want)
		}
	}
}

var checkroutesInvalidTests = []struct {
	routes map[string][]string
	want   string
}{
	{
		map[string][]string{"host:port:invalid": []string{}},
		"invalid source address: address host:port:invalid: too many colons in address",
	},
	{
		map[string][]string{"err": []string{}},
		"cannot resolve host 'err': LookupHost error",
	},
	{
		map[string][]string{"host": []string{"host:port"}},
		"invalid destination 'host:port' for source address 'host': address host:port: invalid port",
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
