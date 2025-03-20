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
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// configV1 represents the configuration for sshproxy v1.x.y.
type configV1 struct {
	Debug                 bool
	Log                   string
	CheckInterval         time.Duration `yaml:"check_interval"`
	ErrorBanner           string        `yaml:"error_banner"`
	Dump                  string
	DumpLimitSize         uint64        `yaml:"dump_limit_size"`
	DumpLimitWindow       time.Duration `yaml:"dump_limit_window"`
	Etcd                  etcdConfig
	EtcdStatsInterval     time.Duration `yaml:"etcd_stats_interval"`
	LogStatsInterval      time.Duration `yaml:"log_stats_interval"`
	BgCommand             string        `yaml:"bg_command"`
	SSH                   sshConfig
	TranslateCommands     map[string]*TranslateCommandConfig `yaml:"translate_commands"`
	Environment           map[string]string
	Routes                map[string]*routeConfigV1
	MaxConnectionsPerUser int `yaml:"max_connections_per_user"`
	Users                 []map[string]subConfigV1
	Groups                []map[string]subConfigV1
}

// routeConfigV1 represents the configuration of a route in v1.x.y. Dest is
// mandatory, Source is mandatory if the associated service name is not the
// default one. RouteSelect defaults to "ordered", Mode defaults to "stiky",
// ForceCommand is optional, CommandMustMatch defaults to false
type routeConfigV1 struct {
	Source           []string
	Dest             []string
	RouteSelect      string `yaml:"route_select"`
	Mode             string
	ForceCommand     string `yaml:"force_command"`
	CommandMustMatch bool   `yaml:"command_must_match"`
	EtcdKeyTTL       int64  `yaml:"etcd_keyttl"`
	Environment      map[string]string
}

// subConfigV1 represents the subConfig used for users and groups in v1.x.y. We
// use interface{} instead of real type to check if the option was specified or
// not.
type subConfigV1 struct {
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
	Routes                map[string]*routeConfigV1
	MaxConnectionsPerUser interface{} `yaml:"max_connections_per_user"`
	SSH                   sshConfig
}

func convertRouteToOverride(subConf *subConfig, routeConfig *routeConfigV1) {
	subConf.Dest = routeConfig.Dest
	if routeConfig.RouteSelect != "" {
		subConf.RouteSelect = routeConfig.RouteSelect
	}
	if routeConfig.Mode != "" {
		subConf.Mode = routeConfig.Mode
	}
	if routeConfig.ForceCommand != "" {
		subConf.ForceCommand = routeConfig.ForceCommand
	}
	if routeConfig.CommandMustMatch {
		subConf.CommandMustMatch = routeConfig.CommandMustMatch
	}
	if routeConfig.EtcdKeyTTL != 0 {
		subConf.EtcdKeyTTL = routeConfig.EtcdKeyTTL
	}
}

func convertNamedRouteToOverride(config *Config, routeConfig *routeConfigV1, routeName string, match map[string][]string) {
	var subConf subConfig
	subConf.Match = []map[string][]string{match}
	subConf.Service = routeName
	convertRouteToOverride(&subConf, routeConfig)
	subConf.Environment = routeConfig.Environment
	config.Overrides = append(config.Overrides, subConf)
}

func convertSubConfigToOverride(config *Config, confsV1 []map[string]subConfigV1, matchWith string) {
	// we have to use a slice of maps in order to have ordered maps
	for _, subConfsV1 := range confsV1 {
		for names, subConfV1 := range subConfsV1 {
			var subConf subConfig
			subConf.Match = []map[string][]string{{matchWith: strings.Split(names, ",")}}
			subConf.Debug = subConfV1.Debug
			subConf.Log = subConfV1.Log
			subConf.ErrorBanner = subConfV1.ErrorBanner
			subConf.Dump = subConfV1.Dump
			subConf.DumpLimitSize = subConfV1.DumpLimitSize
			subConf.DumpLimitWindow = subConfV1.DumpLimitWindow
			subConf.EtcdStatsInterval = subConfV1.EtcdStatsInterval
			subConf.LogStatsInterval = subConfV1.LogStatsInterval
			subConf.BgCommand = subConfV1.BgCommand
			subConf.TranslateCommands = subConfV1.TranslateCommands
			if subConfV1.Environment == nil {
				subConf.Environment = make(map[string]string)
			} else {
				subConf.Environment = subConfV1.Environment
			}
			subConf.MaxConnectionsPerUser = subConfV1.MaxConnectionsPerUser
			if subConfV1.SSH.Exe != "" || len(subConfV1.SSH.Args) > 0 {
				subConf.SSH = subConfV1.SSH
			}

			for routeName, routeConfig := range subConfV1.Routes {
				if routeName == "default" {
					convertRouteToOverride(&subConf, routeConfig)
					// merge environment
					for k, v := range routeConfig.Environment {
						subConf.Environment[k] = v
					}
				}
			}

			config.Overrides = append(config.Overrides, subConf)

			for routeName, routeConfig := range subConfV1.Routes {
				if routeName != "default" {
					match := make(map[string][]string, 2)
					match["sources"] = routeConfig.Source
					if matchWith != "" {
						match[matchWith] = strings.Split(names, ",")
					}
					convertNamedRouteToOverride(config, routeConfig, routeName, match)
				}
			}
		}
	}
}

// ConvertConfigV1 loads v1.x.y configuration file and converts it to v2
func ConvertConfigV1(filename string) ([]byte, error) {
	yamlFile, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	var configV1 configV1

	if err := yaml.Unmarshal(yamlFile, &configV1); err != nil {
		return nil, err
	}

	config.Debug = configV1.Debug
	config.Log = configV1.Log
	config.CheckInterval = configV1.CheckInterval
	config.ErrorBanner = configV1.ErrorBanner
	config.Dump = configV1.Dump
	config.DumpLimitSize = configV1.DumpLimitSize
	config.DumpLimitWindow = configV1.DumpLimitWindow
	config.Etcd = configV1.Etcd
	config.EtcdStatsInterval = configV1.EtcdStatsInterval
	config.LogStatsInterval = configV1.LogStatsInterval
	config.BgCommand = configV1.BgCommand
	config.SSH = configV1.SSH
	config.TranslateCommands = configV1.TranslateCommands
	if configV1.Environment == nil {
		config.Environment = make(map[string]string)
	} else {
		config.Environment = configV1.Environment
	}
	config.MaxConnectionsPerUser = configV1.MaxConnectionsPerUser

	for routeName, routeConfig := range configV1.Routes {
		if routeName == "default" {
			config.Dest = routeConfig.Dest
			if routeConfig.RouteSelect != "" {
				config.RouteSelect = routeConfig.RouteSelect
			}
			if routeConfig.Mode != "" {
				config.Mode = routeConfig.Mode
			}
			if routeConfig.ForceCommand != "" {
				config.ForceCommand = routeConfig.ForceCommand
			}
			if routeConfig.CommandMustMatch {
				config.CommandMustMatch = routeConfig.CommandMustMatch
			}
			if routeConfig.EtcdKeyTTL != 0 {
				config.EtcdKeyTTL = routeConfig.EtcdKeyTTL
			}
			// merge environment
			for k, v := range routeConfig.Environment {
				config.Environment[k] = v
			}
		} else {
			match := make(map[string][]string, 1)
			match["sources"] = routeConfig.Source
			convertNamedRouteToOverride(&config, routeConfig, routeName, match)
		}
	}

	convertSubConfigToOverride(&config, configV1.Groups, "groups")

	convertSubConfigToOverride(&config, configV1.Users, "users")

	return yaml.Marshal(&config)
}
