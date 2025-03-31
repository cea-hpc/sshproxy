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
	"os"
	"testing"

	"gopkg.in/yaml.v2"
)

func openConfigV1(filename string) *configV1 {
	yamlFile, _ := os.ReadFile(filename)
	var configV1 configV1
	yaml.Unmarshal(yamlFile, &configV1)
	return &configV1
}

func BenchmarkConvertRouteToOverride(b *testing.B) {
	configV1 := openConfigV1("../../test/configV1a.yaml")
	var subConf subConfig

	b.Run("", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			convertRouteToOverride(&subConf, configV1.Routes["service1"])
		}
	})
}

func BenchmarkConvertNamedRouteToOverride(b *testing.B) {
	configV1 := openConfigV1("../../test/configV1a.yaml")
	var config Config

	b.Run("", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			convertNamedRouteToOverride(&config, configV1.Routes["service1"], "service1", map[string][]string{"sources": {"192.168.0.2"}})
		}
	})
}

var convertSubConfigToOverrideTests = []struct {
	filename string
}{
	{"../../test/configV1a.yaml"},
	{"../../test/configV1b.yaml"},
}

func BenchmarkConvertSubConfigToOverride(b *testing.B) {
	for _, tt := range convertSubConfigToOverrideTests {
		configV1 := openConfigV1(tt.filename)
		var config Config

		b.Run(tt.filename+"_groups", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				convertSubConfigToOverride(&config, configV1.Groups, "groups")
			}
		})
		b.Run(tt.filename+"_users", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				convertSubConfigToOverride(&config, configV1.Users, "users")
			}
		})
	}
}

var convertConfigV1Tests = []struct {
	filename, want, err string
}{
	{"nonexistingfile.yaml", "", "open nonexistingfile.yaml: no such file or directory"},
	{"../../test/configInvalid.yaml", "", "yaml: found character that cannot start any token"},
	{"../../test/configV1a.yaml", `check_interval: 2m30s
environment:
  TEST: test
  XAUTHORITY: /tmp/.Xauthority_{user}
dest: ['host5:4222']
overrides:
- match:
  - sources:
    - 192.168.0.2
  environment:
    XAUTHORITY: /dev/shm/.Xauthority_{user}
  service: service1
  dest: [host3, host4]
  route_select: bandwidth
  mode: balanced
  force_command: internal-sftp
  command_must_match: true
  etcd_keyttl: 3600
- match:
  - groups:
    - foo
    - bar
  debug: true
  log: /tmp/sshproxy-foo/{user}.log
  ssh:
    args: [-vvv, -Y]
  environment:
    ENV1: /tmp/env
    ENV2: /tmp/foo
  dest: [hostx]
- match:
  - groups:
    - foo
    - bar
    sources:
    - 127.0.0.1
  service: service1
  dest: [hosty]
- match:
  - users:
    - alice
    - bob
  debug: true
  log: /tmp/sshproxy-{user}.log
  dump: /tmp/sshproxy-{user}-{time}.dump
  environment:
    ENV3: /tmp/foo
  dest: [hostz]
- match:
  - sources:
    - 127.0.0.2
    users:
    - alice
    - bob
  environment:
    ENV4: /tmp/foo
  service: service1
  dest: ['hostz:4222']
`, ""},
	{"../../test/configV1b.yaml", `route_select: connections
mode: sticky
force_command: /bin/false
command_must_match: true
etcd_keyttl: 1800
`, ""},
}

func TestConvertConfigV1(t *testing.T) {
	for _, tt := range convertConfigV1Tests {
		got, err := ConvertConfigV1(tt.filename)
		if err == nil && tt.err != "" {
			t.Errorf("got no error, want %s", tt.err)
		} else if err != nil && err.Error() != tt.err {
			t.Errorf("ERROR: %s", err)
		} else if err == nil && string(got) != tt.want {
			t.Errorf("want:\n%s\ngot:\n%s", tt.want, string(got))
		}
	}
}

func BenchmarkConvertConfigV1(b *testing.B) {
	for _, tt := range convertConfigV1Tests {
		b.Run(tt.filename, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ConvertConfigV1(tt.filename)
			}
		})
	}
}
