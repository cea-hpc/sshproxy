package main

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"time"

	"sshproxy/route"
	"sshproxy/utils"

	"gopkg.in/yaml.v2"
)

var (
	defaultSshExe  = "ssh"
	defaultSshArgs = []string{"-q", "-Y"}
)

type sshProxyConfig struct {
	Debug          bool
	Log            string
	Dump           string
	Stats_Interval utils.Duration
	Bg_Command     string
	Route_Choice   string
	Ssh            sshConfig
	Environment    map[string]string
	Routes         map[string][]string
	Users          map[string]subConfig
	Groups         map[string]subConfig
}

type sshConfig struct {
	Exe  string
	Args []string
}

// We use interface{} instead of real type to check if the option was specified
// or not.
type subConfig struct {
	Debug          interface{}
	Log            interface{}
	Dump           interface{}
	Stats_Interval interface{}
	Bg_Command     interface{}
	Route_Choice   interface{}
	Environment    map[string]string
	Routes         map[string][]string
	Ssh            sshConfig
}

func parseSubConfig(config *sshProxyConfig, subconfig *subConfig) {
	if subconfig.Debug != nil {
		config.Debug = subconfig.Debug.(bool)
	}

	if subconfig.Log != nil {
		config.Log = subconfig.Log.(string)
	}

	if subconfig.Dump != nil {
		config.Dump = subconfig.Dump.(string)
	}

	if subconfig.Bg_Command != nil {
		config.Bg_Command = subconfig.Bg_Command.(string)
	}

	if subconfig.Route_Choice != nil {
		config.Route_Choice = subconfig.Route_Choice.(string)
	}

	if subconfig.Ssh.Exe != "" {
		config.Ssh.Exe = subconfig.Ssh.Exe
	}

	if subconfig.Ssh.Args != nil {
		config.Ssh.Args = subconfig.Ssh.Args
	}

	if subconfig.Routes != nil {
		config.Routes = subconfig.Routes
	}

	// merge environment
	for k, v := range subconfig.Environment {
		config.Environment[k] = v
	}
}

type PatternReplacer struct {
	Regexp *regexp.Regexp
	Text   string
}

func replace(src string, replacer *PatternReplacer) string {
	return replacer.Regexp.ReplaceAllString(src, replacer.Text)
}

func loadConfig(config_file, username, sid string, start time.Time, groups map[string]bool) (*sshProxyConfig, error) {
	patterns := map[string]*PatternReplacer{
		"{user}": &PatternReplacer{regexp.MustCompile(`{user}`), username},
		"{sid}":  &PatternReplacer{regexp.MustCompile(`{sid}`), sid},
		"{time}": &PatternReplacer{regexp.MustCompile(`{time}`), start.Format(time.RFC3339Nano)},
	}

	yamlFile, err := ioutil.ReadFile(config_file)
	if err != nil {
		return nil, err
	}

	var config sshProxyConfig
	// if no environment is defined in config it seems to not be allocated
	config.Environment = make(map[string]string)

	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return nil, err
	}

	if len(config.Routes) == 0 {
		return nil, fmt.Errorf("no routes specified")
	}

	if config.Route_Choice == "" {
		config.Route_Choice = route.DefaultAlgorithm
	}

	if config.Ssh.Exe == "" {
		config.Ssh.Exe = defaultSshExe
	}

	if config.Ssh.Args == nil {
		config.Ssh.Args = defaultSshArgs
	}

	for groupname, groupconfig := range config.Groups {
		if groups[groupname] {
			parseSubConfig(&config, &groupconfig)
		}
	}

	if userconfig, present := config.Users[username]; present {
		parseSubConfig(&config, &userconfig)
	}

	if config.Log != "" {
		config.Log = replace(config.Log, patterns["{user}"])
	}

	for k, v := range config.Environment {
		config.Environment[k] = replace(v, patterns["{user}"])
	}

	if !route.IsAlgorithm(config.Route_Choice) {
		return nil, fmt.Errorf("invalid value for `route_choice` option: %s", config.Route_Choice)
	}

	if config.Dump != "" {
		for _, repl := range patterns {
			config.Dump = replace(config.Dump, repl)
		}
	}

	return &config, nil
}
