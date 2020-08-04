// Copyright 2015-2020 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package utils

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"time"

	"gopkg.in/yaml.v2"
)

var (
	defaultSSHExe  = "ssh"
	defaultSSHArgs = []string{"-q", "-Y"}
)

// Config represents the configuration for sshproxy.
type Config struct {
	Debug             bool
	Log               string
	CheckInterval     Duration `yaml:"check_interval"` // Minimum interval between host checks
	Dump              string
	DumpLimitSize     uint64   `yaml:"dump_limit_size"`
	Etcd              etcdConfig
	EtcdStatsInterval Duration `yaml:"etcd_stats_interval"`
	LogStatsInterval  Duration `yaml:"log_stats_interval"`
	BgCommand         string   `yaml:"bg_command"`
	SSH               sshConfig
	Environment       map[string]string
	Routes            map[string]*RouteConfig
	Users             map[string]subConfig
	Groups            map[string]subConfig
}

// RouteConfig represents the configuration of a route. Dest is mandatory,
// Source is mandatory if the associated service name is not the default one.
// RouteSelect defaults to "ordered", Mode defaults to "stiky".
type RouteConfig struct {
	Source      []string
	Dest        []string
	RouteSelect string `yaml:"route_select"`
	Mode        string
}

type sshConfig struct {
	Exe  string
	Args []string
}

type etcdConfig struct {
	Endpoints []string
	TLS       etcdTLSConfig
	Username  string
	Password  string
	KeyTTL    int64
}

type etcdTLSConfig struct {
	CAFile   string
	KeyFile  string
	CertFile string
}

// We use interface{} instead of real type to check if the option was specified
// or not.
type subConfig struct {
	Debug             interface{}
	Log               interface{}
	Dump              interface{}
	DumpLimitSize     interface{} `yaml:"dump_limit_size"`
	EtcdStatsInterval interface{} `yaml:"etcd_stats_interval"`
	LogStatsInterval  interface{} `yaml:"log_stats_interval"`
	BgCommand         interface{} `yaml:"bg_command"`
	Environment       map[string]string
	Routes            map[string]*RouteConfig
	SSH               sshConfig
}

func parseSubConfig(config *Config, subconfig *subConfig) error {
	if subconfig.Debug != nil {
		config.Debug = subconfig.Debug.(bool)
	}

	if subconfig.Log != nil {
		config.Log = subconfig.Log.(string)
	}

	if subconfig.Dump != nil {
		config.Dump = subconfig.Dump.(string)
	}

	if subconfig.DumpLimitSize != nil {
		config.DumpLimitSize = subconfig.DumpLimitSize.(uint64)
	}

	if subconfig.EtcdStatsInterval != nil {
		var err error
		config.EtcdStatsInterval, err = ParseDuration(subconfig.EtcdStatsInterval.(string))
		if err != nil {
			return err
		}
	}

	if subconfig.LogStatsInterval != nil {
		var err error
		config.LogStatsInterval, err = ParseDuration(subconfig.LogStatsInterval.(string))
		if err != nil {
			return err
		}
	}

	if subconfig.BgCommand != nil {
		config.BgCommand = subconfig.BgCommand.(string)
	}

	if subconfig.SSH.Exe != "" {
		config.SSH.Exe = subconfig.SSH.Exe
	}

	if subconfig.SSH.Args != nil {
		config.SSH.Args = subconfig.SSH.Args
	}

	if subconfig.Routes != nil {
		config.Routes = subconfig.Routes
	}

	// merge environment
	for k, v := range subconfig.Environment {
		config.Environment[k] = v
	}

	return nil
}

type patternReplacer struct {
	Regexp *regexp.Regexp
	Text   string
}

func replace(src string, replacer *patternReplacer) string {
	return replacer.Regexp.ReplaceAllString(src, replacer.Text)
}

// LoadConfig load configuration file and adapt it according to specified user.
func LoadConfig(filename, username, sid string, start time.Time, groups map[string]bool) (*Config, error) {
	patterns := map[string]*patternReplacer{
		"{user}": {regexp.MustCompile(`{user}`), username},
		"{sid}":  {regexp.MustCompile(`{sid}`), sid},
		"{time}": {regexp.MustCompile(`{time}`), start.Format(time.RFC3339Nano)},
	}

	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	// if no environment is defined in config it seems to not be allocated
	config.Environment = make(map[string]string)

	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, err
	}

	if config.SSH.Exe == "" {
		config.SSH.Exe = defaultSSHExe
	}

	if config.SSH.Args == nil {
		config.SSH.Args = defaultSSHArgs
	}

	for groupname, groupconfig := range config.Groups {
		if groups[groupname] {
			if err := parseSubConfig(&config, &groupconfig); err != nil {
				return nil, err
			}
		}
	}

	if userconfig, present := config.Users[username]; present {
		if err := parseSubConfig(&config, &userconfig); err != nil {
			return nil, err
		}
	}

	if config.Log != "" {
		config.Log = replace(config.Log, patterns["{user}"])
	}

	for k, v := range config.Environment {
		config.Environment[k] = replace(v, patterns["{user}"])
	}

	// replace sources and destinations (with possible missing port) with host:port.
	if err := CheckRoutes(config.Routes); err != nil {
		return nil, fmt.Errorf("invalid value in `routes` option: %s", err)
	}

	if config.Dump != "" {
		for _, repl := range patterns {
			config.Dump = replace(config.Dump, repl)
		}
	}

	return &config, nil
}
