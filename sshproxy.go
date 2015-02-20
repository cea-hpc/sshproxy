package main

import (
	"crypto/sha1"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"runtime"
	"sync"
	"time"

	"sshproxy/group.go"

	"github.com/docker/docker/pkg/term"
	"github.com/op/go-logging"
)

var VERSION = "0.1.0"

type ChooseDestinationFunc func([]string) (string, string, error)

var (
	routeChoosers = map[string]ChooseDestinationFunc{
		"ordered": chooseDestinationOrdered,
		"random":  chooseDestinationRandom,
	}

	defaultConfig = "/etc/sshproxy.cfg"
)

// main logger for sshproxy
var log = logging.MustGetLogger("sshproxy")

// calcSessionId returns a unique 10 hexadecimal characters string from a user
// name, host, port and timestamp.
func calcSessionId(user, host, port string, ts time.Time) string {
	return fmt.Sprintf("%.5X", sha1.Sum([]byte(fmt.Sprintf("%s@%s:%s@%d", user, host, port, ts.UnixNano()))))
}

// mustSetupLogging setups logging framework for sshproxy.
//
// logfile can be:
//   - empty (""): logs will be written on stdout,
//   - "syslog": logs will be sent to syslog(),
//   - a filename: logs will be appended in this file (the subdirectories will
//     be created if they do not exist).
//
// sid is a unique session id (calculated with calcSessionId) used to identify
// a session in the logs.
// Debug output is enabled if debug is true.
func mustSetupLogging(logfile, sid string, debug bool) {
	var logBackend logging.Backend
	logFormat := fmt.Sprintf("%%{time:2006-01-02 15:04:05} %%{level} %s: %%{message}", sid)
	if logfile == "syslog" {
		var err error
		logBackend, err = logging.NewSyslogBackend("sshproxy")
		if err != nil {
			log.Fatalf("error opening syslog: %s", err)
		}
		logFormat = fmt.Sprintf("%%{level} %s: %%{message}", sid)
	} else {
		var f *os.File
		if logfile == "" {
			f = os.Stderr
		} else {
			err := os.MkdirAll(path.Dir(logfile), 0755)
			if err != nil {
				log.Fatalf("creating directory %s: %s", path.Dir(logfile), err)
			}

			f, err = os.OpenFile(logfile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				log.Fatalf("error opening log file %s: %v", logfile, err)
			}
		}
		logBackend = logging.NewLogBackend(f, "", 0)
	}

	logging.SetBackend(logBackend)
	logging.SetFormatter(logging.MustStringFormatter(logFormat))
	if debug {
		logging.SetLevel(logging.DEBUG, "sshproxy")
	} else {
		logging.SetLevel(logging.NOTICE, "sshproxy")
	}
}

// getGroups returns a map of group memberships for the current user.
//
// It can be used to quickly check if a user is in a specified group.
func getGroups() (map[string]bool, error) {
	gids, err := os.Getgroups()
	if err != nil {
		return nil, err
	}

	groups := make(map[string]bool)
	for _, gid := range gids {
		g, err := group.LookupId(gid)
		if err != nil {
			return nil, err
		}

		groups[g.Name] = true
	}

	return groups, nil
}

// splitHostPort splits a network address of the form "host:port" or
// "host[:port]" into host and port. If the port is not specified the default
// ssh port ("22") is returned.
func splitHostPort(hostport string) (string, string, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		if err.(*net.AddrError).Err == "missing port in address" {
			return hostport, defaultSshPort, nil
		} else {
			return hostport, defaultSshPort, err
		}
	}
	return host, port, nil
}

// canConnect tests if a connection to host:port can be made (with a 1s timeout).
func canConnect(host, port string) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1*time.Second)
	if err != nil {
		log.Info("cannot connect to %s:%s: %s", host, port, err)
		return false
	}
	c.Close()
	return true
}

// chooseDestinationOrdered chooses the first reachable destination from a list
// of destinations. It returns its host and port.
func chooseDestinationOrdered(destinations []string) (string, string, error) {
	for i, dst := range destinations {
		host, port, err := splitHostPort(dst)
		if err != nil {
			return "", "", err
		}

		// always return the last destination without trying to connect
		if i == len(destinations)-1 {
			return host, port, nil
		}
		if canConnect(host, port) {
			return host, port, nil
		}
	}
	return "", "", fmt.Errorf("no valid destination found")
}

// chooseDestinationRandom randomizes the order of the provided list of
// destinations and chooses the first reachable one. It returns its host and
// port.
func chooseDestinationRandom(destinations []string) (string, string, error) {
	rand.Seed(time.Now().UnixNano())
	// Fisher-Yates shuffle: http://en.wikipedia.org/wiki/Fisher%E2%80%93Yates_shuffle
	// In-place shuffle (instead of using rand.Perm()).
	for i := len(destinations) - 1; i > 0; i-- {
		j := rand.Intn(i)
		destinations[i], destinations[j] = destinations[j], destinations[i]
	}
	log.Debug("randomized destinations: %v", destinations)
	return chooseDestinationOrdered(destinations)
}

// findDestination finds a reachable destination for the sshd server according
// to routes. route_choice specifies the algorithm used to choose the
// destination (can be "ordered" or "random").
func findDestination(routes map[string][]string, route_choice, sshd string) (string, string, error) {
	if destinations, present := routes[sshd]; present {
		return routeChoosers[route_choice](destinations)
	} else if destinations, present := routes["default"]; present {
		return routeChoosers[route_choice](destinations)
	}
	return "", "", fmt.Errorf("cannot find a route for %s and no default route configured", sshd)
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

func main() {
	// use all processor cores
	runtime.GOMAXPROCS(runtime.NumCPU())

	defer func() {
		// log error in case of panic()
		if err := recover(); err != nil {
			log.Error("program panicked: %s", err)
		}
	}()

	var err error
	start := time.Now()

	config_file := defaultConfig
	if len(os.Args) > 1 {
		config_file = os.Args[1]
		switch config_file {
		case "-h", "--help":
			fmt.Fprintf(os.Stderr, "usage: sshproxy [--version] [config]\n")
			os.Exit(0)
		case "--version":
			fmt.Fprintf(os.Stderr, "sshproxy version %s\n", VERSION)
			os.Exit(0)
		}
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

	ssh_conn_infos := regexp.MustCompile(`([0-9\.]+) ([0-9]+) ([0-9\.]+) ([0-9]+)`).FindStringSubmatch(ssh_connection)
	if len(ssh_conn_infos) != 5 {
		log.Fatalf("parsing SSH_CONNECTION: bad value '%s'", ssh_connection)
	}

	client_ip, client_port := ssh_conn_infos[1], ssh_conn_infos[2]
	sshd_ip, sshd_port := ssh_conn_infos[3], ssh_conn_infos[4]
	sid := calcSessionId(username, client_ip, client_port, start)

	groups, err := getGroups()
	if err != nil {
		log.Fatalf("Cannot find current user groups: %s", err)
	}

	config, err := loadConfig(config_file, username, start, groups)
	if err != nil {
		log.Fatalf("Reading configuration '%s': %s", config_file, err)
	}

	mustSetupLogging(config.Log, sid, config.Debug)

	log.Debug("groups = %v", groups)
	log.Debug("config.debug = %v", config.Debug)
	log.Debug("config.log = %s", config.Log)
	log.Debug("config.dump = %s", config.Dump)
	log.Debug("config.stats_interval = %s", config.Stats_Interval)
	log.Debug("config.bg_command = %s", config.Bg_Command)
	log.Debug("config.environment = %v", config.Environment)
	log.Debug("config.route_choice = %s", config.Route_Choice)
	log.Debug("config.routes = %v", config.Routes)
	log.Debug("config.ssh.exe = %s", config.Ssh.Exe)
	log.Debug("config.ssh.args = %v", config.Ssh.Args)

	setEnvironment(config.Environment)

	log.Notice("%s connected from %s:%s to sshd listening on %s:%s", username, client_ip, client_port, sshd_ip, sshd_port)
	defer log.Notice("disconnected")

	host, port, err := findDestination(config.Routes, config.Route_Choice, sshd_ip)
	if err != nil {
		log.Fatalf("Finding destination: %s", err)
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

	original_cmd := os.Getenv("SSH_ORIGINAL_COMMAND")
	log.Debug("original_cmd = %s", original_cmd)

	// We assume the `sftp-server` binary is in the same directory on the
	// gateway as on the target.
	ssh_args := config.Ssh.Args
	if port != defaultSshPort {
		ssh_args = append(ssh_args, "-p", port)
	}
	ssh_args = append(ssh_args, host, original_cmd)
	cmd := exec.Command(config.Ssh.Exe, ssh_args...)
	log.Debug("command = %s %q", cmd.Path, cmd.Args)

	recorder, err := NewRecorder(config.Dump, config.Stats_Interval, done)
	if err != nil {
		log.Fatalf("setting recorder: %s", err)
	}

	go func() {
		wg.Add(1)
		defer wg.Done()
		recorder.Run()
	}()

	log.Notice("proxied to %s:%s", host, port)

	if term.IsTerminal(os.Stdout.Fd()) {
		err = runTtyCommand(cmd, done, recorder)
	} else {
		err = runStdCommand(cmd, done, recorder)
	}
	if err != nil {
		log.Error("error executing proxied ssh command: %s", err)
	}
}
