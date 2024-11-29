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
	"net"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cea-hpc/sshproxy/pkg/nodesets"

	"gopkg.in/yaml.v2"
)

var (
	defaultSSHExe  = "ssh"
	defaultSSHArgs = []string{"-q", "-Y"}
	// defaultAlgorithm is the default algorithm used to find a route if no
	// other algorithm is specified in configuration.
	defaultAlgorithm = "ordered"
	// defaultMode is the default mode used to find a route if no other mode is
	// specified in the configuration.
	defaultMode    = "sticky"
	defaultService = "default"
	defaultDest    = []string{}
)

var cachedConfig Config

// Config represents the configuration for sshproxy.
type Config struct {
	ready                 bool
	Nodeset               string
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
	Service               string
	Dest                  []string
	RouteSelect           string `yaml:"route_select"`
	Mode                  string
	ForceCommand          string `yaml:"force_command"`
	CommandMustMatch      bool   `yaml:"command_must_match"`
	EtcdKeyTTL            int64  `yaml:"etcd_keyttl"`
	MaxConnectionsPerUser int    `yaml:"max_connections_per_user"`
	Overrides             []subConfig
}

// TranslateCommandConfig represents the configuration of a translate_command.
// SSHArgs is optional. Command is mandatory. DisableDump defaults to false
type TranslateCommandConfig struct {
	SSHArgs     []string `yaml:"ssh_args"`
	Command     string
	DisableDump bool `yaml:"disable_dump"`
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
	Match                 []map[string][]string
	Debug                 interface{}
	Log                   interface{}
	CheckInterval         interface{} `yaml:"check_interval"`
	ErrorBanner           interface{} `yaml:"error_banner"`
	Dump                  interface{}
	DumpLimitSize         interface{} `yaml:"dump_limit_size"`
	DumpLimitWindow       interface{} `yaml:"dump_limit_window"`
	Etcd                  interface{}
	EtcdStatsInterval     interface{} `yaml:"etcd_stats_interval"`
	LogStatsInterval      interface{} `yaml:"log_stats_interval"`
	BgCommand             interface{} `yaml:"bg_command"`
	SSH                   interface{}
	TranslateCommands     map[string]*TranslateCommandConfig `yaml:"translate_commands"`
	Environment           map[string]string
	Service               interface{}
	Dest                  []string
	RouteSelect           interface{} `yaml:"route_select"`
	Mode                  interface{}
	ForceCommand          interface{} `yaml:"force_command"`
	CommandMustMatch      interface{} `yaml:"command_must_match"`
	EtcdKeyTTL            interface{} `yaml:"etcd_keyttl"`
	MaxConnectionsPerUser interface{} `yaml:"max_connections_per_user"`
}

// Return slice of strings containing formatted configuration values
func PrintConfig(config *Config, groups map[string]bool) []string {
	output := []string{config.Nodeset}
	output = append(output, fmt.Sprintf("groups = %v", groups))
	output = append(output, fmt.Sprintf("config.debug = %v", config.Debug))
	output = append(output, fmt.Sprintf("config.log = %s", config.Log))
	output = append(output, fmt.Sprintf("config.check_interval = %s", config.CheckInterval.Duration()))
	output = append(output, fmt.Sprintf("config.error_banner = %s", config.ErrorBanner))
	output = append(output, fmt.Sprintf("config.dump = %s", config.Dump))
	output = append(output, fmt.Sprintf("config.dump_limit_size = %d", config.DumpLimitSize))
	output = append(output, fmt.Sprintf("config.dump_limit_window = %s", config.DumpLimitWindow.Duration()))
	output = append(output, fmt.Sprintf("config.etcd = %+v", config.Etcd))
	output = append(output, fmt.Sprintf("config.etcd_stats_interval = %s", config.EtcdStatsInterval.Duration()))
	output = append(output, fmt.Sprintf("config.log_stats_interval = %s", config.LogStatsInterval.Duration()))
	output = append(output, fmt.Sprintf("config.bg_command = %s", config.BgCommand))
	output = append(output, fmt.Sprintf("config.ssh = %+v", config.SSH))
	for k, v := range config.TranslateCommands {
		output = append(output, fmt.Sprintf("config.TranslateCommands.%s = %+v", k, v))
	}
	output = append(output, fmt.Sprintf("config.environment = %v", config.Environment))
	output = append(output, fmt.Sprintf("config.service = %s", config.Service))
	output = append(output, fmt.Sprintf("config.dest = %v", config.Dest))
	output = append(output, fmt.Sprintf("config.route_select = %s", config.RouteSelect))
	output = append(output, fmt.Sprintf("config.mode = %s", config.Mode))
	output = append(output, fmt.Sprintf("config.force_command = %s", config.ForceCommand))
	output = append(output, fmt.Sprintf("config.command_must_match = %v", config.CommandMustMatch))
	output = append(output, fmt.Sprintf("config.etcd_keyttl = %d", config.EtcdKeyTTL))
	output = append(output, fmt.Sprintf("config.max_connections_per_user = %d", config.MaxConnectionsPerUser))
	return output
}

func parseSubConfig(config *Config, subconfig *subConfig) error {
	if subconfig.Debug != nil {
		config.Debug = subconfig.Debug.(bool)
	}

	if subconfig.Log != nil {
		config.Log = subconfig.Log.(string)
	}

	if subconfig.CheckInterval != nil {
		var err error
		config.CheckInterval, err = ParseDuration(subconfig.CheckInterval.(string))
		if err != nil {
			return err
		}
	}

	if subconfig.ErrorBanner != nil {
		config.ErrorBanner = subconfig.ErrorBanner.(string)
	}

	if subconfig.Dump != nil {
		config.Dump = subconfig.Dump.(string)
	}

	if subconfig.DumpLimitSize != nil {
		config.DumpLimitSize = uint64(subconfig.DumpLimitSize.(int))
	}

	if subconfig.DumpLimitWindow != nil {
		var err error
		config.DumpLimitWindow, err = ParseDuration(subconfig.DumpLimitWindow.(string))
		if err != nil {
			return err
		}
	}

	if subconfig.Etcd != nil {
		config.Etcd = subconfig.Etcd.(etcdConfig)
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

	if subconfig.SSH != nil {
		config.SSH = subconfig.SSH.(sshConfig)
	}

	// merge translate_commands
	for k, v := range subconfig.TranslateCommands {
		config.TranslateCommands[k] = v
	}

	// merge environment
	for k, v := range subconfig.Environment {
		config.Environment[k] = v
	}

	if subconfig.Service != nil {
		config.Service = subconfig.Service.(string)
	}

	if len(subconfig.Dest) > 0 {
		config.Dest = subconfig.Dest
	}

	if subconfig.RouteSelect != nil {
		config.RouteSelect = subconfig.RouteSelect.(string)
	}

	if subconfig.Mode != nil {
		config.Mode = subconfig.Mode.(string)
	}

	if subconfig.ForceCommand != nil {
		config.ForceCommand = subconfig.ForceCommand.(string)
	}

	if subconfig.CommandMustMatch != nil {
		config.CommandMustMatch = subconfig.CommandMustMatch.(bool)
	}

	if subconfig.EtcdKeyTTL != nil {
		config.EtcdKeyTTL = int64(subconfig.EtcdKeyTTL.(int))
	}

	if subconfig.MaxConnectionsPerUser != nil {
		config.MaxConnectionsPerUser = subconfig.MaxConnectionsPerUser.(int)
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

// LoadConfig load configuration file and adapt it according to specified user/group/sshdHostPort.
func LoadConfig(filename, currentUsername, sid string, start time.Time, groups map[string]bool, sshdHostPort string) (*Config, error) {
	if cachedConfig.ready {
		return &cachedConfig, nil
	}

	patterns := map[string]*patternReplacer{
		"{user}": {regexp.MustCompile(`{user}`), currentUsername},
		"{sid}":  {regexp.MustCompile(`{sid}`), sid},
		"{time}": {regexp.MustCompile(`{time}`), start.Format(time.RFC3339Nano)},
	}

	yamlFile, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	// if no environment is defined in cachedConfig it seems to not be allocated
	cachedConfig.Environment = make(map[string]string)

	if err := yaml.Unmarshal(yamlFile, &cachedConfig); err != nil {
		return nil, err
	}

	for _, override := range cachedConfig.Overrides {
		for _, conditions := range override.Match {
			match := true
			for cType, cValue := range conditions {
				// other cType can be defined as needed. For example
				// environment variables could be useful matches
				if cType == "users" {
					match = slices.Contains(cValue, currentUsername)
				} else if cType == "groups" {
					match = false
					for group := range groups {
						match = slices.Contains(cValue, group)
						if match {
							// no need to go further as match is true and
							// we're in an "or" statement
							break
						}
					}
				} else if cType == "sources" {
					match = false
					if sshdHostPort != "" {
						// sshdHostPort is empty when sshproxyctl is called
						// without the --source option
						for _, source := range cValue {
							match, err = MatchSource(source, sshdHostPort)
							if err != nil {
								return nil, err
							} else if match {
								// no need to go further as match is true and
								// we're in an "or" statement
								break
							}
						}
					}
				}
				if !match {
					// no need to go further as match is false and we're in an
					// "and" statement
					break
				}
			}
			if match {
				// apply the override because we're in an "or" statement
				if err := parseSubConfig(&cachedConfig, &override); err != nil {
					return nil, err
				}
				// no need to to parse the same subconfig twice
				break
			}
		}
	}

	if cachedConfig.Service == "" {
		cachedConfig.Service = defaultService
	}

	if cachedConfig.Dest == nil {
		cachedConfig.Dest = defaultDest
	}

	if cachedConfig.SSH.Exe == "" {
		cachedConfig.SSH.Exe = defaultSSHExe
	}

	if cachedConfig.SSH.Args == nil {
		cachedConfig.SSH.Args = defaultSSHArgs
	}

	if cachedConfig.RouteSelect == "" {
		cachedConfig.RouteSelect = defaultAlgorithm
	}

	if !IsRouteAlgorithm(cachedConfig.RouteSelect) {
		return nil, fmt.Errorf("invalid value for `route_select` option of service '%s': %s", cachedConfig.Service, cachedConfig.RouteSelect)
	}

	if cachedConfig.Mode == "" {
		cachedConfig.Mode = defaultMode
	}

	if !IsRouteMode(cachedConfig.Mode) {
		return nil, fmt.Errorf("invalid value for `mode` option of service '%s': %s", cachedConfig.Service, cachedConfig.Mode)
	}

	if cachedConfig.Log != "" {
		cachedConfig.Log = replace(cachedConfig.Log, patterns["{user}"])
	}

	for k, v := range cachedConfig.Environment {
		cachedConfig.Environment[k] = replace(v, patterns["{user}"])
	}

	if len(cachedConfig.Dest) == 0 {
		return nil, fmt.Errorf("no destination defined for service '%s'", cachedConfig.Service)
	}

	// exand destination nodesets
	nodesetComment, nodesetDlclose, nodesetExpand := nodesets.Functions()
	defer nodesetDlclose()
	cachedConfig.Nodeset = nodesetComment
	dsts, err := nodesetExpand(strings.Join(cachedConfig.Dest, ","))
	if err != nil {
		return nil, fmt.Errorf("invalid nodeset for service '%s': %s", cachedConfig.Service, err)
	}
	cachedConfig.Dest = dsts

	// replace destinations (with possible missing port) with host:port
	for i, dst := range cachedConfig.Dest {
		host, port, err := SplitHostPort(dst)
		if err != nil {
			return nil, fmt.Errorf("invalid destination '%s' for service '%s': %s", dst, cachedConfig.Service, err)
		}
		cachedConfig.Dest[i] = net.JoinHostPort(host, port)
	}

	if cachedConfig.Dump != "" {
		for _, repl := range patterns {
			cachedConfig.Dump = replace(cachedConfig.Dump, repl)
		}
	}

	cachedConfig.ready = true
	return &cachedConfig, nil
}
