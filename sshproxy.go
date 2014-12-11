package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"sshproxy/group.go"

	"github.com/BurntSushi/toml"
	"github.com/docker/docker/pkg/term"
	"github.com/kr/pty"
	"github.com/op/go-logging"
)

type ChooseDestinationFunc func([]string) (string, string, error)

var VERSION = "0.1.0"

var (
	routeChoosers = map[string]ChooseDestinationFunc{
		"ordered": chooseDestinationOrdered,
		"random":  chooseDestinationRandom,
	}

	defaultConfig      = "/etc/sshproxy.cfg"
	defaultRouteChoice = "ordered"

	defaultSshExe  = "ssh"
	defaultSshPort = "22"
	defaultSshArgs = []string{"-q", "-Y"}
)

var log = logging.MustGetLogger("sshproxy")

type duration struct {
	time.Duration
}

func (d *duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

type sshProxyConfig struct {
	Debug          bool
	Log            string
	Dump           string
	Stats_Interval duration
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

type subConfig struct {
	Debug          bool
	Log            string
	Dump           string
	Stats_Interval duration
	Bg_Command     string
	Route_Choice   string
	Environment    map[string]string
	Routes         map[string][]string
	Ssh            sshConfig
}

func runStdCommand(cmd *exec.Cmd, done <-chan struct{}, rec *Recorder) error {
	cmd.Stdin = rec.Stdin
	cmd.Stdout = rec.Stdout
	cmd.Stderr = rec.Stderr
	runCommand(cmd, false, done)
	return nil
}

///////////////////////
// Launch command in a PTY
// from https://github.com/9seconds/ah/blob/master/app/utils/exec.go
func runTtyCommand(cmd *exec.Cmd, done <-chan struct{}, rec *Recorder) error {
	p, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer p.Close()

	hostFd := os.Stdin.Fd()
	oldState, err := term.SetRawTerminal(hostFd)
	if err != nil {
		return err
	}
	defer term.RestoreTerminal(hostFd, oldState)

	monitorTtyResize(hostFd, p.Fd())

	go io.Copy(p, rec.Stdin)
	go io.Copy(rec.Stdout, p)
	go io.Copy(rec.Stderr, p)

	runCommand(cmd, true, done)
	return nil
}

func monitorTtyResize(hostFd uintptr, guestFd uintptr) {
	resizeTty(hostFd, guestFd)

	winchChan := make(chan os.Signal, 1)
	signal.Notify(winchChan, syscall.SIGWINCH)

	go func() {
		for _ = range winchChan {
			resizeTty(hostFd, guestFd)
		}
	}()
}

func resizeTty(hostFd uintptr, guestFd uintptr) {
	winsize, err := term.GetWinsize(hostFd)
	if err != nil {
		return
	}
	term.SetWinsize(guestFd, winsize)
}

type Record struct {
	Fd  int
	Buf []byte
}

type Splitter struct {
	f  *os.File
	fd int
	ch chan<- Record
}

func NewSplitter(f *os.File, ch chan Record) *Splitter {
	return &Splitter{f, int(f.Fd()), ch}
}

func (s *Splitter) Close() error {
	return s.f.Close()
}

func (s *Splitter) Read(p []byte) (int, error) {
	s.ch <- Record{s.fd, p}
	return s.f.Read(p)
}

func (s *Splitter) Write(p []byte) (int, error) {
	s.ch <- Record{s.fd, p}
	return s.f.Write(p)
}

type Recorder struct {
	Stdin, Stdout, Stderr *Splitter
	start                 time.Time
	stats_interval        duration
	totals                map[int]int
	ch                    chan Record
	fdump                 *os.File
	done                  <-chan struct{}
}

func NewRecorder(done <-chan struct{}, start time.Time, dumpfile string, stats_interval duration) (*Recorder, error) {
	var fdump *os.File = nil
	if dumpfile != "" {
		err := os.MkdirAll(path.Dir(dumpfile), 0700)
		if err != nil {
			return nil, fmt.Errorf("creating directory %s: %s", path.Dir(dumpfile), err)
		}

		fdump, err = os.Create(dumpfile)
		if err != nil {
			return nil, fmt.Errorf("creating %s: %s", dumpfile, err)
		}
	}

	ch := make(chan Record)

	return &Recorder{
		Stdin:          NewSplitter(os.Stdin, ch),
		Stdout:         NewSplitter(os.Stdout, ch),
		Stderr:         NewSplitter(os.Stderr, ch),
		start:          start,
		stats_interval: stats_interval,
		totals:         map[int]int{0: 0, 1: 0, 2: 0},
		ch:             ch,
		fdump:          fdump,
		done:           done,
	}, nil
}

func (r *Recorder) Log() {
	t := []string{}
	fds := []string{"stdin", "stdout", "stderr"}
	for fd, name := range fds {
		t = append(t, fmt.Sprintf("%s: %d", name, r.totals[fd]))
	}
	// round to second
	elapsed := time.Duration((time.Now().Sub(r.start) / time.Second) * time.Second)
	log.Notice("bytes transferred in %s: %s", elapsed, strings.Join(t, ", "))
}

func (r *Recorder) Dump(rec Record) {
	if r.fdump == nil {
		return
	}

	buf := new(bytes.Buffer)
	data := []interface{}{
		uint8(rec.Fd),
		uint32(len(rec.Buf)),
	}

	for _, v := range data {
		err := binary.Write(buf, binary.BigEndian, v)
		if err != nil {
			log.Error("binary.Write failed: %s", err)
			return
		}
	}

	_, err := buf.Write(rec.Buf)
	if err != nil {
		log.Error("bytes.Buffer.Write failed: %s", err)
		return
	}

	_, err = buf.WriteTo(r.fdump)
	if err != nil {
		log.Error("writing in %s: %s", r.fdump.Name(), err)
		// XXX close r.fdump?
	}
}

func (r *Recorder) Run() {
	defer func() {
		r.fdump.Close()
		r.Log()
	}()

	if r.stats_interval.Duration != 0 {
		go func() {
			for {
				select {
				case <-time.After(r.stats_interval.Duration):
					r.Log()
				case <-r.done:
					return
				}
			}
		}()
	}

	for {
		select {
		case rec := <-r.ch:
			r.totals[rec.Fd] += len(rec.Buf)
			if r.fdump != nil {
				r.Dump(rec)
			}
		case <-r.done:
			return
		}
	}
}

func mustSetupLogging(logfile, current_user, source string, debug bool) {
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

func parseSubConfig(md *toml.MetaData, config *sshProxyConfig, subconfig *subConfig, subgroup, subname string) {
	if md.IsDefined(subgroup, subname, "debug") {
		config.Debug = subconfig.Debug
	}

	if md.IsDefined(subgroup, subname, "log") {
		config.Log = subconfig.Log
	}

	if md.IsDefined(subgroup, subname, "dump") {
		config.Dump = subconfig.Dump
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

	if md.IsDefined(subgroup, subname, "environment") {
		// merge environment
		for k, v := range subconfig.Environment {
			config.Environment[k] = v
		}
	}
}

func loadConfig(config_file, username string, start time.Time, groups map[string]bool) (*sshProxyConfig, error) {
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
				parseSubConfig(&md, &config, &groupconfig, "groups", groupname)
			}
		}
	}

	if userconfig, present := config.Users[username]; present {
		parseSubConfig(&md, &config, &userconfig, "users", username)
	}

	if config.Log != "" {
		config.Log = regexp.MustCompile(`{user}`).ReplaceAllString(config.Log, username)
	}

	for k, v := range config.Environment {
		config.Environment[k] = regexp.MustCompile(`{user}`).ReplaceAllString(v, username)
	}

	if _, ok := routeChoosers[config.Route_Choice]; !ok {
		return nil, fmt.Errorf("invalid value for `route_choice` option: %s", config.Route_Choice)
	}

	if config.Dump != "" {
		config.Dump = regexp.MustCompile(`{user}`).ReplaceAllString(config.Dump, username)
		config.Dump = regexp.MustCompile(`{time}`).ReplaceAllString(config.Dump, start.Format(time.RFC3339Nano))
	}

	return &config, nil
}

func runCommand(cmd *exec.Cmd, started bool, done <-chan struct{}) {
	if !started {
		if err := cmd.Start(); err != nil {
			log.Error("Error launching command '%s': %s", strings.Join(cmd.Args, " "), err)
			return
		}
	}
	go cmd.Wait()

	for {
		select {
		case <-time.After(1 * time.Second):
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				if !cmd.ProcessState.Success() {
					log.Debug("Command '%s' exited prematurely: %s", strings.Join(cmd.Args, " "), cmd.ProcessState.String())
				}
				return
			}
		case <-done:
			cmd.Process.Kill()
			return
		}
	}
}

type BackgroundCommandLogger struct {
	Prefix string
}

func (b *BackgroundCommandLogger) Write(p []byte) (int, error) {
	lines := strings.Split(bytes.NewBuffer(p).String(), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) != 0 {
			log.Debug("%s: %s", b.Prefix, l)
		}
	}
	return len(p), nil
}

func prepareBackgroundCommand(command string, done <-chan struct{}, debug bool) *exec.Cmd {
	args := strings.Fields(command)
	cmd := exec.Command(args[0], args[1:]...)

	if debug {
		stdout_log := &BackgroundCommandLogger{"bg_command.stdout"}
		stderr_log := &BackgroundCommandLogger{"bg_command.stderr"}
		cmd.Stdout = stdout_log
		cmd.Stderr = stderr_log
	}

	return cmd
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

func findDestination(routes map[string][]string, route_choice, sshd_ip string) (string, string, error) {
	if destinations, present := routes[sshd_ip]; present {
		return routeChoosers[route_choice](destinations)
	} else if destinations, present := routes["default"]; present {
		return routeChoosers[route_choice](destinations)
	}
	return "", "", fmt.Errorf("cannot find a route for %s and no default route configured", sshd_ip)
}

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

	src := fmt.Sprintf("%s:%s", ssh_conn_infos[1], ssh_conn_infos[2])
	sshd_ip, sshd_port := ssh_conn_infos[3], ssh_conn_infos[4]

	groups, err := getGroups()
	if err != nil {
		log.Fatalf("Cannot find current user groups: %s", err)
	}

	config, err := loadConfig(config_file, username, start, groups)
	if err != nil {
		log.Fatalf("Reading configuration '%s': %s", config_file, err)
	}

	mustSetupLogging(config.Log, username, src, config.Debug)

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
	if config.Bg_Command != "" {
		go func() {
			wg.Add(1)
			defer wg.Done()
			cmd := prepareBackgroundCommand(config.Bg_Command, done, config.Debug)
			runCommand(cmd, false, done)
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

	recorder, err := NewRecorder(done, start, config.Dump, config.Stats_Interval)
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
		log.Fatalf("error executing command: %s", err)
	}
}
