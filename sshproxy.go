package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"sshproxy/group.go"

	"github.com/BurntSushi/toml"
	"github.com/op/go-logging"
)

type ChoseDestinationFunc func([]string) (string, string, error)

var (
	routeChosers = map[string]ChoseDestinationFunc{"ordered": choseDestinationOrdered, "random": choseDestinationRandom}

	defaultConfig      = "/etc/sshproxy.cfg"
	defaultRouteChoice = "ordered"

	defaultSshExe  = "ssh"
	defaultSshPort = "22"
	defaultSshArgs = []string{"-q", "-Y"}
)

var log = logging.MustGetLogger("sshproxy")

type sshProxyConfig struct {
	Debug        bool
	Log          string
	Bg_Command   string
	Route_Choice string
	Ssh          sshConfig
	Routes       map[string][]string
	Users        map[string]subConfig
	Groups       map[string]subConfig
}

type sshConfig struct {
	Exe  string
	Args []string
}

type subConfig struct {
	Debug        bool
	Log          string
	Bg_Command   string
	Route_Choice string
	Routes       map[string][]string
	Ssh          sshConfig
}

func MustSetupLogging(logfile, current_user, source string, debug bool) {
	var logBackend logging.Backend
	logFormat := fmt.Sprintf("%%{time:2006-01-02 15:04:05} %%{level} [%s] %%{message}", source)
	if logfile == "syslog" {
		var err error
		logBackend, err = logging.NewSyslogBackend("sshproxy")
		if err != nil {
			log.Fatalf("error opening syslog: %s", err)
		}
		logFormat = fmt.Sprintf("%%{level} [%s@%s] %%{message}", current_user, source)
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

func GetGroups() (map[string]bool, error) {
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

func ParseSubConfig(md *toml.MetaData, config *sshProxyConfig, subconfig *subConfig, subgroup, subname string) {
	if md.IsDefined(subgroup, subname, "debug") {
		config.Debug = subconfig.Debug
	}

	if md.IsDefined(subgroup, subname, "log") {
		config.Log = subconfig.Log
	}

	if md.IsDefined(subgroup, subname, "bg_command") {
		config.Bg_Command = subconfig.Bg_Command
	}

	if md.IsDefined(subgroup, subname, "route_choice") {
		config.Route_Choice = subconfig.Route_Choice
	}

	if md.IsDefined(subgroup, subname, "ssh", "exe") {
		config.Ssh.Exe = subconfig.Ssh.Exe
	}

	if md.IsDefined(subgroup, subname, "ssh", "args") {
		config.Ssh.Args = subconfig.Ssh.Args
	}

	if md.IsDefined(subgroup, subname, "routes") {
		config.Routes = subconfig.Routes
	}
}

func LoadConfig(config_file, username string, groups map[string]bool) (*sshProxyConfig, error) {
	var config sshProxyConfig
	md, err := toml.DecodeFile(config_file, &config)
	if err != nil {
		return nil, err
	}

	if !md.IsDefined("routes") {
		return nil, fmt.Errorf("no routes specified")
	}

	if !md.IsDefined("route_choice") {
		config.Route_Choice = defaultRouteChoice
	}

	if !md.IsDefined("ssh", "exe") {
		config.Ssh.Exe = defaultSshExe
	}

	if !md.IsDefined("ssh", "args") {
		config.Ssh.Args = defaultSshArgs
	}

	for _, key := range md.Keys() {
		if key[0] == "groups" {
			groupname := key[1]
			if groups[groupname] {
				groupconfig := config.Groups[groupname]
				ParseSubConfig(&md, &config, &groupconfig, "groups", groupname)
			}
		}
	}

	if userconfig, present := config.Users[username]; present {
		ParseSubConfig(&md, &config, &userconfig, "users", username)
	}

	if config.Log != "" {
		config.Log = regexp.MustCompile(`{user}`).ReplaceAllString(config.Log, username)
	}

	if _, ok := routeChosers[config.Route_Choice]; !ok {
		return nil, fmt.Errorf("invalid value for `route_choice` option: %s", config.Route_Choice)
	}

	return &config, nil
}

type BackgroundCommandLogger struct {
	Prefix string
}

func (b *BackgroundCommandLogger) Write(p []byte) (int, error) {
	lines := strings.Split(bytes.NewBuffer(p).String(), "\n")
	for _, l := range lines {
		log.Debug("%s: %s", b.Prefix, l)
	}
	return len(p), nil
}

func LaunchBackgroundCommand(command string, done <-chan struct{}, debug bool) {
	if command == "" {
		return
	}

	args := strings.Split(command, " ")
	cmd := exec.Command(args[0], args[1:]...)

	if debug {
		stdout_log := &BackgroundCommandLogger{"bg_command.stdout"}
		stderr_log := &BackgroundCommandLogger{"bg_command.stderr"}
		cmd.Stdout = stdout_log
		cmd.Stderr = stderr_log
	}

	if err := cmd.Start(); err != nil {
		log.Error("Error launching background command: %s", err)
		return
	}

	defer func() {
		// Send a SIGKILL when leaving.
		// XXX Maybe could we send a SIGTERM instead and then a
		// SIGKILL after a timeout?
		cmd.Process.Kill()
		cmd.Wait()
	}()

	<-done
}

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

func canConnect(host, port string) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1*time.Second)
	if err != nil {
		log.Info("cannot connect to %s:%s: %s", host, port, err)
		return false
	}
	c.Close()
	return true
}

func choseDestinationOrdered(destinations []string) (string, string, error) {
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

func choseDestinationRandom(destinations []string) (string, string, error) {
	rand.Seed(time.Now().UnixNano())
	// Fisher-Yates shuffle: http://en.wikipedia.org/wiki/Fisher%E2%80%93Yates_shuffle
	// In-place shuffle (instead of using rand.Perm()).
	for i := len(destinations) - 1; i > 0; i-- {
		j := rand.Intn(i)
		destinations[i], destinations[j] = destinations[j], destinations[i]
	}
	log.Debug("randomized destinations: %v", destinations)
	return choseDestinationOrdered(destinations)
}

func findDestination(routes map[string][]string, route_choice, sshd_ip string) (string, string, error) {
	if destinations, present := routes[sshd_ip]; present {
		return routeChosers[route_choice](destinations)
	} else if destinations, present := routes["default"]; present {
		return routeChosers[route_choice](destinations)
	}
	return "", "", fmt.Errorf("cannot find a route for %s and no default route configured", sshd_ip)
}

func main() {
	// use all processor cores
	runtime.GOMAXPROCS(runtime.NumCPU())

	config_file := defaultConfig
	if len(os.Args) > 1 {
		config_file = os.Args[1]
		if config_file == "-h" || config_file == "--help" {
			fmt.Fprintf(os.Stderr, "usage: sshproxy [config]\n")
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

	src := fmt.Sprintf("%s:%s", ssh_conn_infos[1], ssh_conn_infos[2])
	sshd_ip, sshd_port := ssh_conn_infos[3], ssh_conn_infos[4]

	groups, err := GetGroups()
	if err != nil {
		log.Fatalf("Cannot find current user groups: %s", err)
	}

	config, err := LoadConfig(config_file, username, groups)
	if err != nil {
		log.Fatalf("Reading configuration '%s': %s", config_file, err)
	}

	MustSetupLogging(config.Log, username, src, config.Debug)

	log.Debug("groups = %v", groups)
	log.Debug("config.debug = %v", config.Debug)
	log.Debug("config.log = %s", config.Log)
	log.Debug("config.bg_command = %s", config.Bg_Command)
	log.Debug("config.route_choice = %s", config.Route_Choice)
	log.Debug("config.routes = %v", config.Routes)
	log.Debug("config.ssh.exe = %s", config.Ssh.Exe)
	log.Debug("config.ssh.args = %v", config.Ssh.Args)

	log.Notice("connected to sshd listening on %s:%s", sshd_ip, sshd_port)
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
	go func() {
		wg.Add(1)
		defer wg.Done()
		LaunchBackgroundCommand(config.Bg_Command, done, config.Debug)
	}()

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

	// We can modify those if we want to record session.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Notice("proxied to %s:%s", host, port)

	if err := cmd.Run(); err != nil {
		log.Fatalf("error executing command: %s", err)
	}
}
