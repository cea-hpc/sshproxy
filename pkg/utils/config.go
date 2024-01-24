// Copyright 2015-2024 CEA/DAM/DIF
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
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

var (
	defaultSSHExe  = "ssh"
	defaultSSHArgs = []string{"-q", "-Y"}
	defaultRoutes  = map[string]*RouteConfig{"default": &RouteConfig{
		Dest: []string{}}}
)

// Config represents the configuration for sshproxy.
type Config struct {
	Debug                 bool
	Log                   string
	CheckInterval         Duration `yaml:"check_interval"`
	ErrorBanner           string   `yaml:"error_banner"`
	Dump                  string
	DumpLimitSize         uint64   `yaml:"dump_limit_size"`
	DumpLimitWindow       Duration `yaml:"dump_limit_window"`
	Etcd                  etcdConfig
	EtcdStatsInterval     Duration `yaml:"etcd_stats_interval"`
	LogStatsInterval      Duration `yaml:"log_stats_interval"`
	BgCommand             string   `yaml:"bg_command"`
	SSH                   sshConfig
	TranslateCommands     map[string]*TranslateCommandConfig `yaml:"translate_commands"`
	Environment           map[string]string
	Routes                map[string]*RouteConfig
	MaxConnectionsPerUser int `yaml:"max_connections_per_user"`
	Users                 []map[string]subConfig
	Groups                []map[string]subConfig
}

// TranslateCommandConfig represents the configuration of a translate_command.
// SSHArgs is optional. Command is mandatory. DisableDump defaults to false
type TranslateCommandConfig struct {
	SSHArgs     []string `yaml:"ssh_args"`
	Command     string
	DisableDump bool `yaml:"disable_dump"`
}

// RouteConfig represents the configuration of a route. Dest is mandatory,
// Source is mandatory if the associated service name is not the default one.
// RouteSelect defaults to "ordered", Mode defaults to "stiky", ForceCommand is
// optional, CommandMustMatch defaults to false
type RouteConfig struct {
	Source           []string
	Dest             []string
	RouteSelect      string `yaml:"route_select"`
	Mode             string
	ForceCommand     string `yaml:"force_command"`
	CommandMustMatch bool   `yaml:"command_must_match"`
	EtcdKeyTTL       int64  `yaml:"etcd_keyttl"`
	Environment      map[string]string
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
	Mandatory bool
}

type etcdTLSConfig struct {
	CAFile   string
	KeyFile  string
	CertFile string
}

// We use interface{} instead of real type to check if the option was specified
// or not.
type subConfig struct {
	Debug                 interface{}
	Log                   interface{}
	ErrorBanner           interface{} `yaml:"error_banner"`
	Dump                  interface{}
	DumpLimitSize         interface{}                        `yaml:"dump_limit_size"`
	DumpLimitWindow       interface{}                        `yaml:"dump_limit_window"`
	EtcdStatsInterval     interface{}                        `yaml:"etcd_stats_interval"`
	LogStatsInterval      interface{}                        `yaml:"log_stats_interval"`
	BgCommand             interface{}                        `yaml:"bg_command"`
	TranslateCommands     map[string]*TranslateCommandConfig `yaml:"translate_commands"`
	Environment           map[string]string
	Routes                map[string]*RouteConfig
	MaxConnectionsPerUser interface{} `yaml:"max_connections_per_user"`
	SSH                   sshConfig
}

// Return slice of strings containing formatted configuration values
func PrintConfig(config *Config, groups map[string]bool) []string {
	output := []string{fmt.Sprintf("groups = %v", groups)}
	output = append(output, fmt.Sprintf("config.debug = %v", config.Debug))
	output = append(output, fmt.Sprintf("config.log = %s", config.Log))
	output = append(output, fmt.Sprintf("config.check_interval = %s", config.CheckInterval.Duration()))
	output = append(output, fmt.Sprintf("config.error_banner = %s", config.ErrorBanner))
	output = append(output, fmt.Sprintf("config.dump = %s", config.Dump))
	output = append(output, fmt.Sprintf("config.dump_limit_size = %d", config.DumpLimitSize))
	output = append(output, fmt.Sprintf("config.dump_limit_window = %s", config.DumpLimitWindow.Duration()))
	output = append(output, fmt.Sprintf("config.etcd_stats_interval = %s", config.EtcdStatsInterval.Duration()))
	output = append(output, fmt.Sprintf("config.log_stats_interval = %s", config.LogStatsInterval.Duration()))
	output = append(output, fmt.Sprintf("config.etcd = %+v", config.Etcd))
	output = append(output, fmt.Sprintf("config.bg_command = %s", config.BgCommand))
	for k, v := range config.TranslateCommands {
		output = append(output, fmt.Sprintf("config.TranslateCommands.%s = %+v", k, v))
	}
	output = append(output, fmt.Sprintf("config.environment = %v", config.Environment))
	for k, v := range config.Routes {
		output = append(output, fmt.Sprintf("config.routes.%s = %+v", k, v))
	}
	output = append(output, fmt.Sprintf("config.max_connections_per_user = %d", config.MaxConnectionsPerUser))
	output = append(output, fmt.Sprintf("config.ssh.exe = %s", config.SSH.Exe))
	output = append(output, fmt.Sprintf("config.ssh.args = %v", config.SSH.Args))
	return output
}

func parseSubConfig(config *Config, subconfig *subConfig) error {
	if subconfig.Debug != nil {
		config.Debug = subconfig.Debug.(bool)
	}

	if subconfig.Log != nil {
		config.Log = subconfig.Log.(string)
	}

	if subconfig.ErrorBanner != nil {
		config.ErrorBanner = subconfig.ErrorBanner.(string)
	}

	if subconfig.Dump != nil {
		config.Dump = subconfig.Dump.(string)
	}

	if subconfig.DumpLimitSize != nil {
		config.DumpLimitSize = subconfig.DumpLimitSize.(uint64)
	}

	if subconfig.DumpLimitWindow != nil {
		var err error
		config.DumpLimitWindow, err = ParseDuration(subconfig.DumpLimitWindow.(string))
		if err != nil {
			return err
		}
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

	// merge routes
	if config.Routes == nil {
		config.Routes = defaultRoutes
	}
	for service, opts := range subconfig.Routes {
		config.Routes[service] = opts
	}

	if subconfig.MaxConnectionsPerUser != nil {
		config.MaxConnectionsPerUser = subconfig.MaxConnectionsPerUser.(int)
	}

	// merge translate_commands
	for k, v := range subconfig.TranslateCommands {
		config.TranslateCommands[k] = v
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
func LoadConfig(filename, currentUsername, sid string, start time.Time, groups map[string]bool) (*Config, error) {
	patterns := map[string]*patternReplacer{
		"{user}": {regexp.MustCompile(`{user}`), currentUsername},
		"{sid}":  {regexp.MustCompile(`{sid}`), sid},
		"{time}": {regexp.MustCompile(`{time}`), start.Format(time.RFC3339Nano)},
	}

	yamlFile, err := os.ReadFile(filename)
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

	// we have to use a slice of maps in order to have ordered maps
	for _, groupconfigs := range config.Groups {
		for groupnames, groupconfig := range groupconfigs {
			for _, groupname := range strings.Split(groupnames, ",") {
				if groups[groupname] {
					if err := parseSubConfig(&config, &groupconfig); err != nil {
						return nil, err
					}
					// no need to to parse the same subconfig twice
					break
				}
			}
		}
	}

	// we have to use a slice of maps in order to have ordered maps
	for _, userconfigs := range config.Users {
		for usernames, userconfig := range userconfigs {
			for _, username := range strings.Split(usernames, ",") {
				if username == currentUsername {
					if err := parseSubConfig(&config, &userconfig); err != nil {
						return nil, err
					}
					// no need to to parse the same subconfig twice
					break
				}
			}
		}
	}

	if config.Log != "" {
		config.Log = replace(config.Log, patterns["{user}"])
	}

	for k, v := range config.Environment {
		config.Environment[k] = replace(v, patterns["{user}"])
	}

	for service, opts := range config.Routes {
		for k, v := range opts.Environment {
			config.Routes[service].Environment[k] = replace(v, patterns["{user}"])
		}
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
