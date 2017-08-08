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
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/cea-hpc/sshproxy/manager"
	"github.com/cea-hpc/sshproxy/route"
	"github.com/cea-hpc/sshproxy/utils"

	"github.com/docker/docker/pkg/term"
	"github.com/op/go-logging"
)

var SSHPROXY_VERSION string

var (
	defaultConfig = "/etc/sshproxy/sshproxy.yaml"
)

// main logger for sshproxy
var log = logging.MustGetLogger("sshproxy")

// findDestination finds a reachable destination for the sshd server according
// to the manager if available or the routes and route_select algorithm.
// It returns a string with host:port, an empty string if no destination is
// found or an error if any.
func findDestination(mclient *manager.Client, routes map[string][]string, route_select, sshd_hostport string) (string, error) {
	if mclient != nil {
		dst, err := mclient.Connect()
		if err != nil {
			// disable manager in case of error
			mclient = nil
			log.Error("%s", err)
		} else {
			if dst == "" {
				log.Debug("got empty response from manager")
			} else {
				log.Debug("got response from manager: %s", dst)
			}
			return dst, nil
		}
	}

	checker := new(route.BasicHostChecker)
	if destinations, present := routes[sshd_hostport]; present {
		return route.Select(route_select, destinations, checker)
	} else if destinations, present := routes[route.DefaultRouteKeyword]; present {
		return route.Select(route_select, destinations, checker)
	}
	return "", fmt.Errorf("cannot find a route for %s and no default route configured", sshd_hostport)
}

// setEnvironment sets environment variables from a map whose keys are the
// variable names.
func setEnvironment(environment map[string]string) {
	for k, v := range environment {
		os.Setenv(k, v)
	}
	for _, e := range os.Environ() {
		log.Debug("env = %s", e)
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
	Ssh   *SSHInfo  // SSH source and destination (from SSH_CONNECTION)
}

func main() {
	defer func() {
		// log error in case of panic()
		if err := recover(); err != nil {
			log.Error("program panicked: %s", err)
		}
	}()

	var err error
	start := time.Now()

	versionFlag := flag.Bool("version", false, "show version number and exit")
	flag.Usage = usage
	flag.Parse()

	if *versionFlag {
		fmt.Fprintf(os.Stderr, "sshproxy version %s\n", SSHPROXY_VERSION)
		os.Exit(0)
	}

	config_file := defaultConfig
	if flag.NArg() != 0 {
		config_file = flag.Arg(0)
	}

	current_user, err := user.Current()
	if err != nil {
		log.Fatalf("Cannot find current user: %s", err)
	}
	username := current_user.Username

	ssh_connection := os.Getenv("SSH_CONNECTION")
	if ssh_connection == "" {
		log.Fatal("No SSH_CONNECTION environment variable")
	}

	ssh_infos, err := NewSSHInfo(ssh_connection)
	if err != nil {
		log.Fatalf("parsing SSH_CONNECTION '%s': %s", ssh_connection, err)
	}

	conninfo := &ConnInfo{
		Start: start,
		User:  username,
		Ssh:   ssh_infos,
	}

	sid := utils.CalcSessionId(conninfo.User, conninfo.Start, conninfo.Ssh.Src())

	groups, err := utils.GetGroups()
	if err != nil {
		log.Fatalf("Cannot find current user groups: %s", err)
	}

	config, err := loadConfig(config_file, username, sid, start, groups)
	if err != nil {
		log.Fatalf("Reading configuration '%s': %s", config_file, err)
	}

	logformat := fmt.Sprintf("%%{time:2006-01-02 15:04:05} %%{level} %s: %%{message}", sid)
	syslogformat := fmt.Sprintf("%%{level} %s: %%{message}", sid)
	utils.MustSetupLogging("sshproxy", config.Log, logformat, syslogformat, config.Debug)

	log.Debug("groups = %v", groups)
	log.Debug("config.debug = %v", config.Debug)
	log.Debug("config.log = %s", config.Log)
	log.Debug("config.dump = %s", config.Dump)
	log.Debug("config.stats_interval = %s", config.Stats_Interval.Duration())
	log.Debug("config.bg_command = %s", config.Bg_Command)
	log.Debug("config.manager = %s", config.Manager)
	log.Debug("config.environment = %v", config.Environment)
	log.Debug("config.route_select = %s", config.Route_Select)
	log.Debug("config.routes = %v", config.Routes)
	log.Debug("config.ssh.exe = %s", config.Ssh.Exe)
	log.Debug("config.ssh.args = %v", config.Ssh.Args)

	setEnvironment(config.Environment)

	log.Notice("%s connected from %s to sshd listening on %s", username, ssh_infos.Src(), ssh_infos.Dst())
	defer log.Notice("disconnected")

	var mclient *manager.Client
	if config.Manager != "" {
		mclient = manager.NewClient(config.Manager, username, ssh_infos.Dst(), 2*time.Second)
		defer func() {
			if mclient != nil {
				mclient.Disconnect()
			}
		}()
	}

	hostport, err := findDestination(mclient, config.Routes, config.Route_Select, ssh_infos.Dst())
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
	done := make(chan struct{})
	defer func() {
		close(done)
		wg.Wait()
	}()

	// launch background command
	if config.Bg_Command != "" {
		go func() {
			wg.Add(1)
			defer wg.Done()
			cmd := prepareBackgroundCommand(config.Bg_Command, config.Debug)
			if err := runCommand(cmd, false, done); err != nil {
				log.Error("error running background command: %s", err)
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
					os.Exit(0)
				}
			case <-done:
				return
			}
		}
	}()

	original_cmd := os.Getenv("SSH_ORIGINAL_COMMAND")
	log.Debug("original_cmd = %s", original_cmd)

	// We assume the `sftp-server` binary is in the same directory on the
	// gateway as on the target.
	ssh_args := config.Ssh.Args
	if port != utils.DefaultSshPort {
		ssh_args = append(ssh_args, "-p", port)
	}
	if original_cmd != "" {
		ssh_args = append(ssh_args, host, original_cmd)
	} else {
		ssh_args = append(ssh_args, host)
	}
	cmd := exec.Command(config.Ssh.Exe, ssh_args...)
	log.Debug("command = %s %q", cmd.Path, cmd.Args)

	interactiveCommand := term.IsTerminal(os.Stdout.Fd())

	var recorder *Recorder
	if !interactiveCommand || config.Dump != "" {
		recorder, err = NewRecorder(conninfo, config.Dump, original_cmd, config.Stats_Interval.Duration(), done)
		if err != nil {
			log.Fatalf("setting recorder: %s", err)
		}

		go func() {
			wg.Add(1)
			defer wg.Done()
			recorder.Run()
		}()
	}

	log.Notice("proxied to %s", hostport)

	if interactiveCommand {
		err = runTtyCommand(cmd, done, recorder)
	} else {
		err = runStdCommand(cmd, done, recorder)
	}
	if err != nil {
		log.Error("error executing proxied ssh command: %s", err)
	}
}
