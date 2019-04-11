// Copyright 2015-2019 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
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

	"github.com/cea-hpc/sshproxy/pkg/etcd"
	"github.com/cea-hpc/sshproxy/pkg/route"
	"github.com/cea-hpc/sshproxy/pkg/utils"

	"github.com/docker/docker/pkg/term"
	"github.com/op/go-logging"
)

var (
	// SshproxyVersion is set in the Makefile.
	SshproxyVersion = "0.0.0+notproperlybuilt"
	defaultConfig   = "/etc/sshproxy/sshproxy.yaml"
)

// main logger for sshproxy
var log = logging.MustGetLogger("sshproxy")

type etcdChecker struct {
	LastState     etcd.State
	checkInterval utils.Duration
	cli           *etcd.Client
}

func (c *etcdChecker) Check(hostport string) bool {
	ts := time.Now()
	var host *etcd.Host
	var err error
	if c.cli != nil && c.cli.IsAlive() {
		host, err = c.cli.GetHost(hostport)
	} else {
		host = &etcd.Host{}
	}

	switch {
	case err != nil:
		if err != etcd.ErrKeyNotFound {
			log.Errorf("problem with etcd: %v", err)
		}
		c.LastState = c.doCheck(hostport)
	case host.State == etcd.Disabled:
		c.LastState = host.State
	case ts.Sub(host.Ts) > c.checkInterval.Duration():
		c.LastState = c.doCheck(hostport)
	default:
		c.LastState = host.State
	}
	return c.LastState == etcd.Up
}

func (c *etcdChecker) doCheck(hostport string) etcd.State {
	ts := time.Now()
	state := etcd.Down
	if route.CanConnect(hostport) {
		state = etcd.Up
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
// It returns a string with host:port, an empty string if no destination is
// found or an error if any.
func findDestination(cli *etcd.Client, username string, routes map[string][]string, routeSelect, sshdHostport string, checkInterval utils.Duration) (string, error) {
	key := fmt.Sprintf("%s@%s", username, sshdHostport)
	checker := &etcdChecker{
		checkInterval: checkInterval,
		cli:           cli,
	}

	if cli != nil && cli.IsAlive() {
		dest, err := cli.GetDestination(key)
		if err != nil {
			if err != etcd.ErrKeyNotFound {
				log.Errorf("problem with etcd: %v", err)
			}
		} else {
			if checker.Check(dest) {
				log.Debugf("found destination in etcd: %s", dest)
				return dest, nil
			}
			log.Infof("cannot connect %s to already existing connection(s) to %s: host %s", key, dest, checker.LastState)
		}
	}

	if destinations, present := routes[sshdHostport]; present {
		return route.Select(routeSelect, destinations, checker)
	} else if destinations, present := routes[route.DefaultRouteKeyword]; present {
		return route.Select(routeSelect, destinations, checker)
	}

	return "", fmt.Errorf("cannot find a route for %s and no default route configured", sshdHostport)
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

//
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

	log.Debugf("groups = %v", groups)
	log.Debugf("config.debug = %v", config.Debug)
	log.Debugf("config.log = %s", config.Log)
	log.Debugf("config.check_interval = %s", config.CheckInterval.Duration())
	log.Debugf("config.dump = %s", config.Dump)
	log.Debugf("config.stats_interval = %s", config.StatsInterval.Duration())
	log.Debugf("config.etcd = %+v", config.Etcd)
	log.Debugf("config.bg_command = %s", config.BgCommand)
	log.Debugf("config.environment = %v", config.Environment)
	log.Debugf("config.route_select = %s", config.RouteSelect)
	log.Debugf("config.routes = %v", config.Routes)
	log.Debugf("config.ssh.exe = %s", config.SSH.Exe)
	log.Debugf("config.ssh.args = %v", config.SSH.Args)

	setEnvironment(config.Environment)

	log.Infof("%s connected from %s to sshd listening on %s", username, sshInfos.Src(), sshInfos.Dst())
	defer log.Info("disconnected")

	cli, err := etcd.NewClient(config, log)
	if err != nil {
		log.Errorf("Cannot contact etcd cluster to update state: %v", err)
	}

	hostport, err := findDestination(cli, username, config.Routes, config.RouteSelect, sshInfos.Dst(), config.CheckInterval)
	switch {
	case err != nil:
		log.Fatalf("Finding destination: %s", err)
	case hostport == "":
		log.Fatal("Cannot find a valid destination")
	}
	host, port, err := utils.SplitHostPort(hostport)
	if err != nil {
		log.Fatalf("Invalid destination '%s': %s", hostport, err)
	}

	// waitgroup and channel to stop our background command when exiting.
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		wg.Wait()
	}()

	sigChannel := make(chan os.Signal, 1)
	signal.Notify(sigChannel, os.Interrupt, os.Kill, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		s := <-sigChannel
		log.Infof("Got signal %s, exiting", s)
		cancel()
	}()

	// Register destination in etcd and keep it alive while running.
	if cli != nil && cli.IsAlive() {
		key := fmt.Sprintf("%s@%s", username, sshInfos.Dst())
		keepAliveChan, err := cli.SetDestination(ctx, key, hostport)
		if err != nil {
			log.Warningf("setting destination in etcd: %v", err)
		}
		go func() {
			wg.Add(1)
			defer wg.Done()
			for {
				select {
				case <-keepAliveChan:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// launch background command
	if config.BgCommand != "" {
		go func() {
			wg.Add(1)
			defer wg.Done()
			cmd := prepareBackgroundCommand(config.BgCommand, config.Debug)
			if err := runCommand(ctx, cmd, false); err != nil {
				log.Errorf("error running background command: %s", err)
			}
		}()
	}

	// Launch goroutine which exits sshproxy if it's attached to PID 1
	// (which means its ssh parent connection is dead).
	go func() {
		wg.Add(1)
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

	// We assume the `sftp-server` binary is in the same directory on the
	// gateway as on the target.
	sshArgs := config.SSH.Args
	envSshproxyArgs := strings.Fields(os.Getenv("SSHPROXY_ARGS"))
	if len(envSshproxyArgs) != 0 {
		sshArgs = append(sshArgs, envSshproxyArgs...)
	}
	if port != utils.DefaultSSHPort {
		sshArgs = append(sshArgs, "-p", port)
	}
	if originalCmd != "" {
		if interactiveCommand {
			// Force TTY allocation because the user probably asked for it.
			sshArgs = append(sshArgs, "-t")
		}
		sshArgs = append(sshArgs, host, originalCmd)
	} else {
		sshArgs = append(sshArgs, host)
	}
	cmd := exec.Command(config.SSH.Exe, sshArgs...)
	log.Debugf("command = %s %q", cmd.Path, cmd.Args)

	var recorder *Recorder
	if !interactiveCommand || config.Dump != "" {
		recorder, err = NewRecorder(ctx, conninfo, config.Dump, originalCmd, config.StatsInterval.Duration())
		if err != nil {
			log.Fatalf("setting recorder: %s", err)
		}

		go func() {
			wg.Add(1)
			defer wg.Done()
			recorder.Run()
		}()
	}

	log.Infof("proxied to %s", hostport)

	if interactiveCommand {
		err = runTtyCommand(ctx, cmd, recorder)
	} else {
		err = runStdCommand(ctx, cmd, recorder)
	}
	if err != nil {
		log.Errorf("error executing proxied ssh command: %s", err)
	}

	// return command exit code
	return cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
}
