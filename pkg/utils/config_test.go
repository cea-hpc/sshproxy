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
	"fmt"
	"reflect"
	"regexp"
	"testing"
	"time"
)

var start time.Time = time.Now()

var replaceTests = []struct {
	option, pattern, want string
}{
	{"This is {test1} a test", "{test1}", "This is abcd a test"},
	{"This is {test2} a test", "{test2}", "This is 1234 a test"},
	{"This is {test2} a {test2} test", "{test2}", "This is 1234 a 1234 test"},
	{"This is {test1} a test", "{test2}", "This is {test1} a test"},
	{"This is {test1} a {test2} test", "{test2}", "This is {test1} a 1234 test"},
}

func patternsForTests() map[string]*patternReplacer {
	patterns := map[string]*patternReplacer{
		"{test1}": {regexp.MustCompile(`{test1}`), "abcd"},
		"{test2}": {regexp.MustCompile(`{test2}`), "1234"},
	}
	return patterns
}

func TestReplace(t *testing.T) {
	patterns := patternsForTests()
	for _, tt := range replaceTests {
		got := replace(tt.option, patterns[tt.pattern])
		if got != tt.want {
			t.Errorf("want: %s, got %s", tt.want, got)
		}
	}
}

func BenchmarkReplace(b *testing.B) {
	patterns := patternsForTests()
	for n, tt := range replaceTests {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				replace(tt.option, patterns[tt.pattern])
			}
		})
	}
}

var loadConfigTests = []struct {
	filename, username string
	want               []string
	err                string
}{
	{"nonexistingfile.yaml", "alice", []string{}, "open nonexistingfile.yaml: no such file or directory"},
	{"../../test/configEmpty.yaml", "alice", []string{
		"libnodeset.so not found, falling back to iskylite's implementation",
		"groups = map[bar:true foo:true]",
		"config.debug = false",
		"config.log = ",
		"config.check_interval = 0s",
		"config.error_banner = ",
		"config.dump = ",
		"config.dump_limit_size = 0",
		"config.dump_limit_window = 0s",
		"config.etcd = {Endpoints:[] TLS:{CAFile: KeyFile: CertFile:} Username: Password: KeyTTL:0 Mandatory:<nil>}",
		"config.etcd_stats_interval = 0s",
		"config.log_stats_interval = 0s",
		"config.blocking_command = ",
		"config.bg_command = ",
		"config.ssh = {Exe:ssh Args:[-q -Y]}",
		"config.environment = map[]",
		"config.service = default",
		"config.dest = []",
		"config.route_select = ordered",
		"config.mode = sticky",
		"config.force_command = ",
		"config.command_must_match = false",
		"config.etcd_keyttl = 0",
		"config.max_connections_per_user = 0",
	}, ""},
	{"../../test/configInvalid.yaml", "alice", []string{}, "yaml: found character that cannot start any token"},
	{"../../test/configCheckIntervalError.yaml", "alice", []string{}, `time: invalid duration "not a duration"`},
	{"../../test/configCheckIntervalNotString.yaml", "alice", []string{}, "check_interval: 10 is not a string"},
	{"../../test/configDumpLimitWindowError.yaml", "alice", []string{}, `time: invalid duration "not a duration"`},
	{"../../test/configDumpLimitWindowNotString.yaml", "alice", []string{}, "dump_limit_window: 10 is not a string"},
	{"../../test/configEtcdStatsIntervalError.yaml", "alice", []string{}, `time: invalid duration "not a duration"`},
	{"../../test/configEtcdStatsIntervalNotString.yaml", "alice", []string{}, "etcd_stats_interval: 10 is not a string"},
	{"../../test/configLogStatsIntervalError.yaml", "alice", []string{}, `time: invalid duration "not a duration"`},
	{"../../test/configLogStatsIntervalNotString.yaml", "alice", []string{}, "log_stats_interval: 10 is not a string"},
	{"../../test/configMatchSourceError.yaml", "alice", []string{}, "source: invalid address: address 127.0.0.1:abcd: invalid port"},
	{"../../test/configRouteSelectError.yaml", "alice", []string{}, "invalid value for `route_select` option of service 'default': notarouteselect"},
	{"../../test/configModeError.yaml", "alice", []string{}, "invalid value for `mode` option of service 'default': notamode"},
	// yes, "cannont" is an upstream typo
	{"../../test/configDestNodesetError.yaml", "alice", []string{}, "invalid nodeset for service 'default': cannont convert ending range to integer a - rangeset parse error"},
	{"../../test/configDestError.yaml", "alice", []string{}, "invalid destination '127.0.0.1:abcd' for service 'default': address 127.0.0.1:abcd: invalid port"},
	{"../../test/configDefault.yaml", "alice", []string{
		"libnodeset.so not found, falling back to iskylite's implementation",
		"groups = map[bar:true foo:true]",
		"config.debug = false",
		"config.log = ",
		"config.check_interval = 0s",
		"config.error_banner = ",
		"config.dump = ",
		"config.dump_limit_size = 0",
		"config.dump_limit_window = 0s",
		"config.etcd = {Endpoints:[] TLS:{CAFile: KeyFile: CertFile:} Username: Password: KeyTTL:0 Mandatory:<nil>}",
		"config.etcd_stats_interval = 0s",
		"config.log_stats_interval = 0s",
		"config.blocking_command = ",
		"config.bg_command = ",
		"config.ssh = {Exe:ssh Args:[-q -Y]}",
		"config.environment = map[]",
		"config.service = default",
		"config.dest = [127.0.0.1:22]",
		"config.route_select = ordered",
		"config.mode = sticky",
		"config.force_command = ",
		"config.command_must_match = false",
		"config.etcd_keyttl = 0",
		"config.max_connections_per_user = 0",
	}, ""},
	{"../../test/config.yaml", "alice", []string{
		"libnodeset.so not found, falling back to iskylite's implementation",
		"groups = map[bar:true foo:true]",
		"config.debug = true",
		"config.log = /tmp/sshproxy-foo/alice.log",
		"config.check_interval = 2m0s",
		"config.error_banner = an other error banner",
		"config.dump = /tmp/sshproxy-alice-" + start.Format(time.RFC3339Nano) + ".dump",
		"config.dump_limit_size = 20",
		"config.dump_limit_window = 3m0s",
		"config.etcd = {Endpoints:[host2] TLS:{CAFile:ca2.pem KeyFile:cert2.key CertFile:cert2.pem} Username:test2 Password:pass2 KeyTTL:2 Mandatory:false}",
		"config.etcd_stats_interval = 4m0s",
		"config.log_stats_interval = 5m0s",
		"config.blocking_command = /a/blocking/command",
		"config.bg_command = /a/background/command",
		"config.ssh = {Exe:sshhhhh Args:[-vvv -Y]}",
		"config.TranslateCommands.acommand = &{SSHArgs:[] Command:something DisableDump:true}",
		"config.TranslateCommands.internal-sftp = &{SSHArgs:[-s] Command:anothercommand DisableDump:false}",
		"config.environment = map[ENV1:/tmp/env XAUTHORITY:/dev/shm/.Xauthority_alice]",
		"config.service = service5",
		"config.dest = [server1:12345]",
		"config.route_select = bandwidth",
		"config.mode = balanced",
		"config.force_command = acommand",
		"config.command_must_match = false",
		"config.etcd_keyttl = 0",
		"config.max_connections_per_user = 0",
	}, ""},
	{"../../test/config.yaml", "notalice", []string{
		"libnodeset.so not found, falling back to iskylite's implementation",
		"groups = map[bar:true foo:true]",
		"config.debug = true",
		"config.log = /var/log/sshproxy/notalice.log",
		"config.check_interval = 2m30s",
		"config.error_banner = an error banner",
		"config.dump = /var/lib/sshproxy/dumps/notalice/" + start.Format(time.RFC3339Nano) + "-abcd.dump",
		"config.dump_limit_size = 10",
		"config.dump_limit_window = 2m31s",
		"config.etcd = {Endpoints:[host1:port1 host2:port2] TLS:{CAFile:ca.pem KeyFile:cert.key CertFile:cert.pem} Username:test Password:pass KeyTTL:5 Mandatory:true}",
		"config.etcd_stats_interval = 2m33s",
		"config.log_stats_interval = 2m32s",
		"config.blocking_command = ",
		"config.bg_command = ",
		"config.ssh = {Exe:ssh Args:[-q -Y]}",
		"config.TranslateCommands.internal-sftp = &{SSHArgs:[-oForwardX11=no -oForwardAgent=no -oPermitLocalCommand=no -oClearAllForwardings=yes -oProtocol=2 -s] Command:sftp DisableDump:true}",
		"config.environment = map[XAUTHORITY:/dev/shm/.Xauthority_notalice]",
		"config.service = default",
		"config.dest = [host5:4222]",
		"config.route_select = ordered",
		"config.mode = sticky",
		"config.force_command = ",
		"config.command_must_match = true",
		"config.etcd_keyttl = 3600",
		"config.max_connections_per_user = 50",
	}, ""},
}

func TestLoadConfig(t *testing.T) {
	sid := "abcd"
	groups := map[string]bool{"foo": true, "bar": true}
	sshdHostPort := "127.0.0.1:22"
	for _, tt := range loadConfigTests {
		cachedConfig = Config{}
		config, err := LoadConfig(tt.filename, tt.username, sid, start, groups, sshdHostPort)
		if err == nil && tt.err != "" {
			t.Errorf("got no error, want %s", tt.err)
		} else if err != nil && err.Error() != tt.err {
			t.Errorf("ERROR: %s, want %s", err, tt.err)
		} else if err == nil && !reflect.DeepEqual(PrintConfig(config, groups), tt.want) {
			t.Errorf("want:\n%v\ngot:\n%v", tt.want, PrintConfig(config, groups))
		} else if err == nil {
			cachedConfig, err := LoadConfig(tt.filename, tt.username, sid, start, groups, sshdHostPort)
			if err != nil {
				t.Errorf("ERROR: %s", err)
			} else if config != cachedConfig {
				t.Error("config and cachedConfig should be the same")
			}
		}
	}
}

func BenchmarkLoadConfig(b *testing.B) {
	sid := "abcd"
	groups := map[string]bool{"foo": true, "bar": true}
	sshdHostPort := "127.0.0.1:22"
	for _, tt := range loadConfigTests {
		b.Run(tt.filename, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cachedConfig = Config{}
				LoadConfig(tt.filename, tt.username, sid, start, groups, sshdHostPort)
			}
		})
	}
}
