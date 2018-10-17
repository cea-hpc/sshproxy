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
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cea-hpc/sshproxy/manager"
	"github.com/cea-hpc/sshproxy/route"
	"github.com/cea-hpc/sshproxy/utils"

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

// findDestination finds a reachable destination for the sshd server according
// to the manager if available or the routes and route_select algorithm.
// It returns a string with host:port, an empty string if no destination is
// found or an error if any.
func findDestination(mclient *manager.Client, routes map[string][]string, routeSelect, sshdHostport string) (string, error) {
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
	SSH   *SSHInfo  // SSH source and destination (from SSH_CONNECTION)
}

func main() {
	os.Exit(mainExitCode())
}

func mainExitCode() int {
	defer func() {
		// log error in case of panic()
		if err := recover(); err != nil {
			log.Error("program panicked: %s", err)
			log.Error("Stack: %s", debug.Stack())
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

	config, err := loadConfig(configFile, username, sid, start, groups)
	if err != nil {
		log.Fatalf("Reading configuration '%s': %s", configFile, err)
	}

	logformat := fmt.Sprintf("%%{time:2006-01-02 15:04:05} %%{level} %s: %%{message}", sid)
	syslogformat := fmt.Sprintf("%%{level} %s: %%{message}", sid)
	utils.MustSetupLogging("sshproxy", config.Log, logformat, syslogformat, config.Debug)

	log.Debug("groups = %v", groups)
	log.Debug("config.debug = %v", config.Debug)
	log.Debug("config.log = %s", config.Log)
	log.Debug("config.dump = %s", config.Dump)
	log.Debug("config.stats_interval = %s", config.StatsInterval.Duration())
	log.Debug("config.bg_command = %s", config.BgCommand)
	log.Debug("config.manager = %s", config.Manager)
	log.Debug("config.environment = %v", config.Environment)
	log.Debug("config.route_select = %s", config.RouteSelect)
	log.Debug("config.routes = %v", config.Routes)
	log.Debug("config.ssh.exe = %s", config.SSH.Exe)
	log.Debug("config.ssh.args = %v", config.SSH.Args)

	setEnvironment(config.Environment)

	log.Info("%s connected from %s to sshd listening on %s", username, sshInfos.Src(), sshInfos.Dst())
	defer log.Info("disconnected")

	var mclient *manager.Client
	if config.Manager != "" {
		mclient = manager.NewClient(config.Manager, username, sshInfos.Dst(), 2*time.Second)
		defer func() {
			if mclient != nil {
				mclient.Disconnect()
			}
		}()
	}

	hostport, err := findDestination(mclient, config.Routes, config.RouteSelect, sshInfos.Dst())
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

	// launch background command
	if config.BgCommand != "" {
		go func() {
			wg.Add(1)
			defer wg.Done()
			cmd := prepareBackgroundCommand(config.BgCommand, config.Debug)
			if err := runCommand(ctx, cmd, false); err != nil {
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
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	originalCmd := os.Getenv("SSH_ORIGINAL_COMMAND")
	log.Debug("original command = %s", originalCmd)

	interactiveCommand := term.IsTerminal(os.Stdout.Fd())
	log.Debug("interactiveCommand = %v", interactiveCommand)

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
	log.Debug("command = %s %q", cmd.Path, cmd.Args)

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

	log.Info("proxied to %s", hostport)

	if interactiveCommand {
		err = runTtyCommand(ctx, cmd, recorder)
	} else {
		err = runStdCommand(ctx, cmd, recorder)
	}
	if err != nil {
		log.Error("error executing proxied ssh command: %s", err)
	}

	cmd.Wait()

	// return command exit code
	return cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
}
