// Copyright 2015-2024 CEA/DAM/DIF
//  Author: Arnaud Guignard <arnaud.guignard@cea.fr>
//  Contributor: Cyril Servant <cyril.servant@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cea-hpc/sshproxy/pkg/utils"

	"github.com/moby/term"
	"github.com/op/go-logging"
	"go.etcd.io/etcd/client/v3"
)

var (
	// SshproxyVersion is set in the Makefile.
	SshproxyVersion = "0.0.0+notproperlybuilt"
	defaultConfig   = "/etc/sshproxy/sshproxy.yaml"
)

// main logger for sshproxy
var log = logging.MustGetLogger("sshproxy")

type etcdChecker struct {
	LastState     utils.State
	checkInterval utils.Duration
	cli           *utils.Client
}

func (c *etcdChecker) Check(hostport string) bool {
	ts := time.Now()
	var host *utils.Host
	var err error
	if c.cli != nil && c.cli.IsAlive() {
		host, err = c.cli.GetHost(hostport)
	} else {
		host = &utils.Host{}
	}

	switch {
	case err != nil:
		if err != utils.ErrKeyNotFound {
			log.Errorf("problem with etcd: %v", err)
		}
		c.LastState = c.doCheck(hostport)
	case host.State == utils.Disabled:
		c.LastState = host.State
	case ts.Sub(host.Ts) > c.checkInterval.Duration():
		c.LastState = c.doCheck(hostport)
	default:
		c.LastState = host.State
	}
	return c.LastState == utils.Up
}

func (c *etcdChecker) doCheck(hostport string) utils.State {
	ts := time.Now()
	state := utils.Down
	if utils.CanConnect(hostport) {
		state = utils.Up
	}
	if c.cli != nil && c.cli.IsAlive() {
		if err := c.cli.SetHost(hostport, state, ts); err != nil {
			log.Errorf("setting host state in etcd: %v", err)
		}
	}
	return state
}

// findDestination finds a reachable destination for the sshd server according
// to the etcd database if available or the routes and route_select algorithm.
// It returns a string with the service name, a string with host:port, a string
// containing ForceCommand value, a bool containing CommandMustMatch, an int64
// with etcd_keyttl and a map of strings with the environment variables; a
// string with the service name and an empty string if no destination is found
// or an error if any.
func findDestination(cli *utils.Client, username string, routes map[string]*utils.RouteConfig, sshdHostport string, checkInterval utils.Duration) (string, string, string, bool, int64, map[string]string, error) {
	checker := &etcdChecker{
		checkInterval: checkInterval,
		cli:           cli,
	}

	service, err := findService(routes, sshdHostport)
	if err != nil {
		return "", "", "", false, 0, nil, err
	}
	key := fmt.Sprintf("%s@%s", username, service)

	if routes[service].Mode == "sticky" && cli != nil && cli.IsAlive() {
		dest, err := cli.GetDestination(key, routes[service].EtcdKeyTTL)
		if err != nil {
			if err != utils.ErrKeyNotFound {
				log.Errorf("problem with etcd: %v", err)
			}
		} else {
			if utils.IsDestinationInRoutes(dest, routes[service].Dest) {
				if checker.Check(dest) {
					log.Debugf("found destination in etcd: %s", dest)
					return service, dest, routes[service].ForceCommand, routes[service].CommandMustMatch, routes[service].EtcdKeyTTL, routes[service].Environment, nil
				}
				log.Infof("cannot connect %s to already existing connection(s) to %s: host %s", key, dest, checker.LastState)
			} else {
				log.Infof("cannot connect %s to already existing connection(s) to %s: not in routes", key, dest)
			}
		}
	}

	if len(routes[service].Dest) > 0 {
		selected, err := utils.SelectRoute(routes[service].RouteSelect, routes[service].Dest, checker, cli, key)
		return service, selected, routes[service].ForceCommand, routes[service].CommandMustMatch, routes[service].EtcdKeyTTL, routes[service].Environment, err
	}

	return service, "", "", false, 0, nil, fmt.Errorf("no destination set for service %s", service)
}

// findService finds the first service containing a suitable source in the conf,
// and returns its name as a string. If it doesn't find any and there is a
// default service, returns the string "default". If no service is found,
// returns an error.
func findService(routes map[string]*utils.RouteConfig, sshdHostport string) (string, error) {
	for service, opts := range routes {
		if len(opts.Source) > 0 {
			for _, source := range opts.Source {
				if source == sshdHostport {
					return service, nil
				}
			}
		}
	}
	if _, ok := routes[utils.DefaultService]; ok {
		return utils.DefaultService, nil
	}
	return "", fmt.Errorf("cannot find a service for %s and no default service configured", sshdHostport)
}

// setEnvironment sets environment variables from a map whose keys are the
// variable names.
func setEnvironment(environment map[string]string) {
	for k, v := range environment {
		os.Setenv(k, v)
	}
	for _, e := range os.Environ() {
		log.Debugf("env = %s", e)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: sshproxy [config]\n")
	flag.PrintDefaults()
	os.Exit(2)
}

// SSHInfo represents the SSH connection information provided by the
// environment variable SSH_CONNECTION.
type SSHInfo struct {
	SrcIP, DstIP     net.IP
	SrcPort, DstPort int
}

// NewSSHInfo parse a string with the same format as the environment variable
// SSH_CONNECTION.
func NewSSHInfo(s string) (*SSHInfo, error) {
	infos := regexp.MustCompile(`([0-9a-f\.:]+) ([0-9]+) ([0-9a-f\.:]+) ([0-9]+)`).FindStringSubmatch(s)
	if len(infos) != 5 {
		return nil, errors.New("bad value")
	}

	srcip := net.ParseIP(infos[1])
	if srcip == nil {
		return nil, errors.New("bad value for source IP")
	}
	srcport, err := strconv.Atoi(infos[2])
	if err != nil {
		return nil, errors.New("bad value for source port")
	}
	dstip := net.ParseIP(infos[3])
	if dstip == nil {
		return nil, errors.New("bad value for destination IP")
	}
	dstport, err := strconv.Atoi(infos[4])
	if err != nil {
		return nil, errors.New("bad value for destination port")
	}

	return &SSHInfo{
		SrcIP:   srcip,
		SrcPort: srcport,
		DstIP:   dstip,
		DstPort: dstport,
	}, nil
}

// Src returns the source address with the format host:port.
func (s *SSHInfo) Src() string {
	return net.JoinHostPort(s.SrcIP.String(), strconv.Itoa(s.SrcPort))
}

// Dst returns the destination address with the format host:port.
func (s *SSHInfo) Dst() string {
	return net.JoinHostPort(s.DstIP.String(), strconv.Itoa(s.DstPort))
}

// ConnInfo regroups specific information about a connection.
type ConnInfo struct {
	Start time.Time // start time
	User  string    // user name
	SSH   *SSHInfo  // SSH source and destination (from SSH_CONNECTION)
}

func main() {
	os.Exit(mainExitCode())
}

func mainExitCode() int {
	defer func() {
		// log error in case of panic()
		if err := recover(); err != nil {
			log.Errorf("program panicked: %s", err)
			log.Errorf("Stack: %s", debug.Stack())
		}
	}()

	var err error
	start := time.Now()

	versionFlag := flag.Bool("version", false, "show version number and exit")
	flag.Usage = usage
	flag.Parse()

	if *versionFlag {
		fmt.Fprintf(os.Stderr, "sshproxy version %s\n", SshproxyVersion)
		return 0
	}

	configFile := defaultConfig
	if flag.NArg() != 0 {
		configFile = flag.Arg(0)
	}

	currentUser, err := user.Current()
	if err != nil {
		log.Fatalf("Cannot find current user: %s", err)
	}
	username := currentUser.Username

	sshConnection := os.Getenv("SSH_CONNECTION")
	if sshConnection == "" {
		log.Fatal("No SSH_CONNECTION environment variable")
	}

	sshInfos, err := NewSSHInfo(sshConnection)
	if err != nil {
		log.Fatalf("parsing SSH_CONNECTION '%s': %s", sshConnection, err)
	}

	conninfo := &ConnInfo{
		Start: start,
		User:  username,
		SSH:   sshInfos,
	}

	sid := utils.CalcSessionID(conninfo.User, conninfo.Start, conninfo.SSH.Src())

	groups, err := utils.GetGroups()
	if err != nil {
		log.Fatalf("Cannot find current user groups: %s", err)
	}

	config, err := utils.LoadConfig(configFile, username, sid, start, groups)
	if err != nil {
		log.Fatalf("Reading configuration '%s': %s", configFile, err)
	}

	logformat := fmt.Sprintf("%%{time:2006-01-02 15:04:05} %%{level} %s: %%{message}", sid)
	syslogformat := fmt.Sprintf("%%{level} %s: %%{message}", sid)
	utils.MustSetupLogging("sshproxy", config.Log, logformat, syslogformat, config.Debug)

	for _, configLine := range utils.PrintConfig(config, groups) {
		log.Debug(configLine)
	}

	log.Infof("%s connected from %s to sshd listening on %s", username, sshInfos.Src(), sshInfos.Dst())
	defer log.Info("disconnected")

	cli, err := utils.NewEtcdClient(config, log)
	if err != nil {
		log.Errorf("Cannot contact etcd cluster to update state: %v", err)
	}

	if cli != nil && cli.IsAlive() {
		if config.MaxConnectionsPerUser > 0 {
			userConnectionsCount, err := cli.GetUserConnectionsCount(username)
			if err != nil {
				log.Fatalf("Getting user connections count: %s", err)
			}
			log.Debugf("Number of connections of %s: %d", username, userConnectionsCount)
			if userConnectionsCount >= config.MaxConnectionsPerUser {
				fmt.Fprintln(os.Stderr, "Too many simultaneous connections")
				log.Fatalf("Max connections per user reached for %s", username)
			}
		}
	} else {
		if config.Etcd.Mandatory {
			log.Fatal("Etcd is mandatory but unavailable")
		}
	}

	service, hostport, forceCommand, commandMustMatch, etcdKeyTTL, environment, err := findDestination(cli, username, config.Routes, sshInfos.Dst(), config.CheckInterval)
	switch {
	case err != nil:
		log.Fatalf("Finding destination: %s", err)
	case hostport == "":
		errorBanner := ""
		if cli != nil && cli.IsAlive() {
			errorBanner, _, _ = cli.GetErrorBanner()
		}
		if errorBanner == "" {
			errorBanner = config.ErrorBanner
		}
		if errorBanner != "" {
			fmt.Println(errorBanner)
		}
		log.Fatal("Cannot find a valid destination")
	}
	host, port, err := utils.SplitHostPort(hostport)
	if err != nil {
		log.Fatalf("Invalid destination '%s': %s", hostport, err)
	}

	log.Debugf("service = %s", service)

	// merge service environment with global environment
	for k, v := range environment {
		config.Environment[k] = v
	}
	setEnvironment(config.Environment)

	// waitgroup and channel to stop our background command when exiting.
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		wg.Wait()
	}()

	sigChannel := make(chan os.Signal, 1)
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		s := <-sigChannel
		log.Infof("Got signal %s, exiting", s)
		cancel()
	}()

	var etcdPath string
	var tmpKeepAliveChan <-chan *clientv3.LeaseKeepAliveResponse
	// Register destination in etcd and keep it alive while running.
	if cli != nil && cli.IsAlive() {
		key := fmt.Sprintf("%s@%s", username, service)
		keepAliveChan, eP, err := cli.SetDestination(ctx, key, sshInfos.Dst(), hostport, etcdKeyTTL)
		etcdPath = eP
		if err != nil {
			log.Warningf("setting destination in etcd: %v", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case isChanAlive := <-keepAliveChan:
					if isChanAlive == nil {
						cli.Disable()
						tmpKeepAliveChan, err = cli.NewLease(ctx)
						if err != nil {
							log.Warningf("getting a new lease in etcd: %v", err)
						} else {
							keepAliveChan = tmpKeepAliveChan
							cli.Enable()
						}
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// launch background command
	if config.BgCommand != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := prepareBackgroundCommand(ctx, config.BgCommand, config.Debug)
			if _, err := runCommand(cmd, false); err != nil {
				select {
				case <-ctx.Done():
					// stay silent as the session is now finished
				default:
					log.Errorf("error running background command: %s", err)
				}
			}
		}()
	}

	// Launch goroutine which exits sshproxy if it's attached to PID 1
	// (which means its ssh parent connection is dead).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-time.After(1 * time.Second):
				if os.Getppid() == 1 {
					log.Warning("SSH parent connection is dead")
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	originalCmd := os.Getenv("SSH_ORIGINAL_COMMAND")
	log.Debugf("original command = %s", originalCmd)

	interactiveCommand := term.IsTerminal(os.Stdout.Fd())
	log.Debugf("interactiveCommand = %v", interactiveCommand)

	sshArgs := config.SSH.Args
	envSshproxyArgs := strings.Fields(os.Getenv("SSHPROXY_ARGS"))
	if len(envSshproxyArgs) != 0 {
		sshArgs = append(sshArgs, envSshproxyArgs...)
	}
	if port != utils.DefaultSSHPort {
		sshArgs = append(sshArgs, "-p", port)
	}
	doCmd := ""
	if forceCommand != "" {
		log.Debugf("forceCommand = %s", forceCommand)
		doCmd = forceCommand
	} else if originalCmd != "" {
		doCmd = originalCmd
	}
	commandTranslated := false
	if doCmd != "" {
		if commandMustMatch && originalCmd != doCmd {
			log.Errorf("error executing proxied ssh command: originalCmd \"%s\" does not match forceCommand \"%s\"", originalCmd, forceCommand)
			return 1
		}
		for fromCmd, translateCmdConf := range config.TranslateCommands {
			if doCmd == fromCmd {
				log.Debugf("translateCmdConf = %+v", translateCmdConf)
				sshArgs = append(sshArgs, translateCmdConf.SSHArgs...)
				sshArgs = append(sshArgs, host, "--", translateCmdConf.Command)
				if config.Dump != "" && translateCmdConf.DisableDump {
					config.Dump = "etcd"
				}
				commandTranslated = true
				break
			}
		}
		if !commandTranslated {
			if interactiveCommand {
				// Force TTY allocation because the user probably asked for it.
				sshArgs = append(sshArgs, "-t")
			}
			sshArgs = append(sshArgs, host, "--", doCmd)
		}
	} else {
		sshArgs = append(sshArgs, host)
	}
	cmd := exec.CommandContext(ctx, config.SSH.Exe, sshArgs...)
	log.Debugf("command = %s %q", cmd.Path, cmd.Args)

	var recorder *Recorder
	if config.Dump != "" {
		recorder = NewRecorder(conninfo, config.Dump, doCmd, config.EtcdStatsInterval.Duration(), config.LogStatsInterval.Duration(), config.DumpLimitSize, config.DumpLimitWindow.Duration())

		wg.Add(1)
		go func() {
			defer wg.Done()
			recorder.Run(ctx, cli, etcdPath)
		}()
	}

	log.Infof("proxied to %s (service: %s)", hostport, service)

	var rc int
	if interactiveCommand {
		rc, err = runTtyCommand(cmd, recorder)
	} else {
		rc, err = runStdCommand(cmd, recorder)
	}
	if err != nil {
		log.Errorf("error executing proxied ssh command: %s", err)
	}

	// return command exit code
	return rc
}
