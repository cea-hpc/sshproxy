// Copyright 2015-2017 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package main

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"time"

	"github.com/cea-hpc/sshproxy/route"
	"github.com/cea-hpc/sshproxy/utils"

	"gopkg.in/yaml.v2"
)

var (
	defaultSSHExe  = "ssh"
	defaultSSHArgs = []string{"-q", "-Y"}
)

type sshProxyConfig struct {
	Debug         bool
	Log           string
	Dump          string
	StatsInterval utils.Duration `yaml:"stats_interval"`
	BgCommand     string         `yaml:"bg_command"`
	Manager       string
	RouteSelect   string `yaml:"route_select"`
	SSH           sshConfig
	Environment   map[string]string
	Routes        map[string][]string
	Users         map[string]subConfig
	Groups        map[string]subConfig
}

type sshConfig struct {
	Exe  string
	Args []string
}

// We use interface{} instead of real type to check if the option was specified
// or not.
type subConfig struct {
	Debug         interface{}
	Log           interface{}
	Dump          interface{}
	StatsInterval interface{} `yaml:"stats_interval"`
	BgCommand     interface{} `yaml:"bg_command"`
	Manager       interface{}
	RouteSelect   interface{} `yaml:"route_select"`
	Environment   map[string]string
	Routes        map[string][]string
	SSH           sshConfig
}

func parseSubConfig(config *sshProxyConfig, subconfig *subConfig) error {
	if subconfig.Debug != nil {
		config.Debug = subconfig.Debug.(bool)
	}

	if subconfig.Log != nil {
		config.Log = subconfig.Log.(string)
	}

	if subconfig.Dump != nil {
		config.Dump = subconfig.Dump.(string)
	}

	if subconfig.StatsInterval != nil {
		var err error
		config.StatsInterval, err = utils.ParseDuration(subconfig.StatsInterval.(string))
		if err != nil {
			return err
		}
	}

	if subconfig.BgCommand != nil {
		config.BgCommand = subconfig.BgCommand.(string)
	}

	if subconfig.Manager != nil {
		config.Manager = subconfig.Manager.(string)
	}

	if subconfig.RouteSelect != nil {
		config.RouteSelect = subconfig.RouteSelect.(string)
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

func loadConfig(filename, username, sid string, start time.Time, groups map[string]bool) (*sshProxyConfig, error) {
	patterns := map[string]*patternReplacer{
		"{user}": &patternReplacer{regexp.MustCompile(`{user}`), username},
		"{sid}":  &patternReplacer{regexp.MustCompile(`{sid}`), sid},
		"{time}": &patternReplacer{regexp.MustCompile(`{time}`), start.Format(time.RFC3339Nano)},
	}

	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config sshProxyConfig
	// if no environment is defined in config it seems to not be allocated
	config.Environment = make(map[string]string)

	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, err
	}

	if config.RouteSelect == "" {
		config.RouteSelect = route.DefaultAlgorithm
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

	if !route.IsAlgorithm(config.RouteSelect) {
		return nil, fmt.Errorf("invalid value for `route_select` option: %s", config.RouteSelect)
	}

	// replace sources and destinations (with possible missing port) with host:port.
	if err := utils.CheckRoutes(config.Routes); err != nil {
		return nil, fmt.Errorf("invalid value in `routes` option: %s", err)
	}

	if config.Dump != "" {
		for _, repl := range patterns {
			config.Dump = replace(config.Dump, repl)
		}
	}

	return &config, nil
}
