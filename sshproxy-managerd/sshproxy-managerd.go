// Copyright 2015 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	"sshproxy/route"
	"sshproxy/utils"

	"github.com/op/go-logging"
	"gopkg.in/yaml.v2"
)

var SSHPROXY_VERSION string

var (
	defaultConfig     = "/etc/sshproxy/sshproxy-managerd.yaml"
	defaultListenAddr = "127.0.0.1:55555"
)

var (
	invalidHostError        = errors.New("invalid host specified")
	notEnoughArgumentsError = errors.New("not enough arguments")
)

var (
	// main logger for sshproxy-managerd
	log = logging.MustGetLogger("sshproxy-managerd")

	// configuration
	config managerdConfig

	// host checker keeping a pool of alive hosts.
	managerHostChecker = NewHostChecker()

	// map of proxied connections (keys are user@host)
	proxiedConnections = make(map[string]*proxiedConn)
)

// Configuration
type managerdConfig struct {
	Debug          bool                 // Debug mode
	Listen         string               // Listen address [host]:port
	Log            string               // Where to log: empty is for stdout, "syslog" or a file
	Check_Interval utils.Duration       // Minimum interval between host checks
	Route_Select   string               // Algorithm used to select a destination
	Routes         map[string][]string  // Routes definition
	Groups         map[string]subConfig // Groups overriden options
	Users          map[string]subConfig // Users overriden options
}

// sub-configuration for users/groups
type subConfig struct {
	Route_Select string
	Routes       map[string][]string
}

// loadConfig loads configuration from a file name and saves it in the config
// global variable.
func loadConfig(config_file string) error {
	yamlFile, err := ioutil.ReadFile(config_file)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(yamlFile, &config); err != nil {
		return err
	}

	if len(config.Routes) == 0 {
		return fmt.Errorf("no routes specified")
	}

	if config.Listen == "" {
		config.Listen = defaultListenAddr
	}

	if config.Route_Select == "" {
		config.Route_Select = route.DefaultAlgorithm
	}

	return nil
}

// State of an host
type State int

const (
	Up       State = iota // host is up
	Down                  // host is down
	Disabled              // host is disabled
)

// Names associated with states
var StateNames = map[State]string{
	Up:       "up",
	Down:     "down",
	Disabled: "disabled",
}

// hostState represents the result of an host check.
type hostState struct {
	State State     // host state (see State for available states)
	Ts    time.Time // time of last check
}

// hostChecker implements the sshproxy.route.HostChecker interface. It is used
// to keep a view of hosts state and to check their availability only after a
// defined duration (set in the config.Check_Interval global variable).
type hostChecker struct {
	States map[string]*hostState // map with "host:port" as keys and their associated state
}

// NewHostChecker creates a new hostChecker.
func NewHostChecker() *hostChecker {
	return &hostChecker{make(map[string]*hostState)}
}

// Check checks if an host is enabled and alive.
//
// It looks for it in its internal view. If found and its last check is less
// than config.Check_Interval duration it returns its known state. Otherwise it
// performs a check and updates (or adds a state to) the internal view
// accordingly.
func (hc *hostChecker) Check(host, port string) bool {
	ts := time.Now()
	dst := net.JoinHostPort(host, port)
	var state State
	s, ok := hc.States[dst]
	switch {
	case !ok:
		state = hc.DoCheck(host, port)
	case s.State == Disabled:
		state = s.State
	case ts.Sub(s.Ts) > config.Check_Interval.Duration():
		state = hc.DoCheck(host, port)
	default:
		state = s.State
	}
	return state == Up
}

// DoCheck checks if an host is alive and updates the internal view.
func (hc *hostChecker) DoCheck(host, port string) State {
	state := Down
	if route.CanConnect(host, port) {
		state = Up
	}
	hc.Update(host, port, state, time.Now())
	return state
}

// Update updates (or creates) the state of an host in the internal view.
func (hc *hostChecker) Update(host, port string, state State, ts time.Time) {
	dst := net.JoinHostPort(host, port)
	if s, ok := hc.States[dst]; ok {
		s.State = state
		s.Ts = ts
	} else {
		s = &hostState{
			State: state,
			Ts:    ts,
		}
		hc.States[dst] = s
	}
}

// IsDisabled checks if an host is disabled.
func (hc *hostChecker) IsDisabled(host, port string) bool {
	if s, ok := hc.States[net.JoinHostPort(host, port)]; ok {
		return s.State == Disabled
	}
	return false
}

// proxiedConn represents the details of a proxied connection for a couple
// (user, host).
type proxiedConn struct {
	Dest string    // Chosen destination
	N    int       // Number of connections
	Ts   time.Time // Start of last connection
}

// genKey returns the key used in the proxiedConnections global variable.
func genKey(user, host string) string {
	return fmt.Sprintf("%s@%s", user, host)
}

// getAlgorithmAndRoutes returns the selection algorithm and a slice with the
// possible destinations from the global configuration for a user connected to
// an hostport and belonging to groups.
func getAlgorithmAndRoutes(user, hostport string, groups map[string]bool) (string, []string) {
	configs := []*subConfig{}

	// add main config
	configs = append(configs, &subConfig{Route_Select: config.Route_Select, Routes: config.Routes})
	// add group configs
	for g, cfg := range config.Groups {
		if groups[g] {
			configs = append(configs, &cfg)
		}
	}
	// add user config
	if cfg, ok := config.Users[user]; ok {
		configs = append(configs, &cfg)
	}

	algo := ""
	dests := []string{}
	host, port, _ := utils.SplitHostPort(hostport)

	for _, cfg := range configs {
		if cfg.Route_Select != "" {
			algo = cfg.Route_Select
		}
		if d, ok := cfg.Routes[hostport]; ok {
			dests = d
		} else if d, ok := cfg.Routes[host]; port == utils.DefaultSshPort && ok {
			dests = d
		} else if d, ok := cfg.Routes[route.DefaultRouteKeyword]; ok {
			dests = d
		}
	}

	return algo, dests
}

// selectRoute returns a destination for a user connected to an hostport. The
// destination may or may not be available (e.g. if there is only one possible
// destination, its connectivity is not checked).
func selectRoute(user, hostport string) (string, error) {
	groups, err := utils.GetGroupList(user)
	if err != nil {
		return "", fmt.Errorf("cannot find groups for user '%s': %s", user, err)
	}

	algo, dests := getAlgorithmAndRoutes(user, hostport, groups)

	dhost, dport, err := route.Select(algo, dests, managerHostChecker)
	if err != nil {
		return "", fmt.Errorf("cannot select route for user '%s': %s", user, err)
	}

	return net.JoinHostPort(dhost, dport), nil
}

// commandHandler represents an handler for a protocol command.
type commandHandler func([]string) (string, error)

// commandHandlers associates available handlers for known commands.
var commandHandlers = map[string]commandHandler{
	"connect":    connectHandler,
	"disable":    disableHandler,
	"disconnect": disconnectHandler,
	"enable":     enableHandler,
	"info":       infoHandler,
	"failure":    failureHandler,
}

// connectHandler handles the "connect user host[:port]" command.
//
// It returns a destination as first value or an empty string in case of error
// (with the error as second value).
func connectHandler(args []string) (string, error) {
	if len(args) != 2 {
		return "", notEnoughArgumentsError
	}

	user, hostport := args[0], args[1]
	if _, _, err := utils.SplitHostPort(hostport); err != nil {
		return "", invalidHostError
	}

	key := genKey(user, hostport)
	pc, ok := proxiedConnections[key]
	if ok {
		log.Info("found connection for %s: %s", key, pc.Dest)
		host, port, _ := utils.SplitHostPort(pc.Dest)
		if managerHostChecker.Check(host, port) {
			pc.N++
			pc.Ts = time.Now()
			return pc.Dest, nil
		} else {
			log.Info("cannot connect %s to already existing connection(s) to %s: host down or disabled", key, pc.Dest)
			if !managerHostChecker.IsDisabled(host, port) {
				managerHostChecker.Update(host, port, Down, time.Now())
			}
		}
	}

	dest, err := selectRoute(user, hostport)
	if err != nil {
		return "", err
	}

	proxiedConnections[key] = &proxiedConn{
		Dest: dest,
		N:    1,
		Ts:   time.Now(),
	}

	log.Info("new connection for %s: %s", key, dest)
	return dest, nil
}

// disableHandler handles the "disable host:port command.
//
// It always returns an empty string as first value.
// In case of error, the error is returned as second value.
func disableHandler(args []string) (string, error) {
	if len(args) != 1 {
		return "", notEnoughArgumentsError
	}

	hostport := args[0]
	host, port, err := utils.SplitHostPort(hostport)
	if err != nil {
		return "", invalidHostError
	}
	managerHostChecker.Update(host, port, Disabled, time.Now())

	return "", nil
}

// disconnectHandler handles the "disconnect user host[:port]" command.
//
// It always returns an empty string as first value.
// In case of error, the error is returned as second value.
func disconnectHandler(args []string) (string, error) {
	if len(args) != 2 {
		return "", notEnoughArgumentsError
	}

	key := genKey(args[0], args[1])
	pc, ok := proxiedConnections[key]
	if !ok {
		return "", fmt.Errorf("key is not present: %s", key)
	}

	pc.N--
	if pc.N == 0 {
		log.Info("no more active connection for %s (to %s): removing", key, pc.Dest)
		delete(proxiedConnections, key)
	}

	return "", nil
}

// enableHandler handles the "enable host:port" command.
//
// It always returns an empty string as first value.
// In case of error, the error is returned as second value.
func enableHandler(args []string) (string, error) {
	if len(args) != 1 {
		return "", notEnoughArgumentsError
	}

	hostport := args[0]
	host, port, err := utils.SplitHostPort(hostport)
	if err != nil {
		return "", invalidHostError
	}

	if managerHostChecker.IsDisabled(host, port) {
		managerHostChecker.DoCheck(host, port)
	} else {
		return "", fmt.Errorf("host %s is already enabled", hostport)
	}

	return "", nil
}

// infoHandler handles the "info (connections|checks)" command.
//
// It returns the infos as first value or an empty string in case of error
// (with the error as second value).
func infoHandler(args []string) (string, error) {
	if len(args) == 0 {
		return "", notEnoughArgumentsError
	}

	var lines []string
	switch strings.ToLower(args[0]) {
	case "connections":
		lines = make([]string, len(proxiedConnections))
		i := 0
		for k, v := range proxiedConnections {
			lines[i] = fmt.Sprintf("id=%s dest=%s n=%d ts=%s", k, v.Dest, v.N, v.Ts.Format(time.RFC3339Nano))
			i++
		}
	case "checks":
		lines = make([]string, len(managerHostChecker.States))
		i := 0
		for k, v := range managerHostChecker.States {
			lines[i] = fmt.Sprintf("host=%s state=%s ts=%s", k, StateNames[v.State], v.Ts.Format(time.RFC3339Nano))
			i++
		}
	default:
		return "", fmt.Errorf("unknown parameter: %s", args[0])
	}

	return strings.Join(lines, "\r\n"), nil
}

// failureHandler handles the "failure host[:port]" command.
//
// It always returns an empty string as first value.
// In case of error, the error is returned as second value.
func failureHandler(args []string) (string, error) {
	if len(args) != 1 {
		return "", notEnoughArgumentsError
	}

	hostport := args[0]
	host, port, err := utils.SplitHostPort(hostport)
	if err != nil {
		return "", invalidHostError
	}

	// Check host before marking it down
	if !managerHostChecker.IsDisabled(host, port) {
		if managerHostChecker.DoCheck(host, port) == Up {
			return "", fmt.Errorf("%s is alive", hostport)
		}
	} else {
		return "", fmt.Errorf("%s is disabled")
	}

	return "", nil
}

// request represents a request from a client.
type request struct {
	request  string      // the request sent by the client
	errc     chan error  // channel to write a possible error
	response chan string // channel to write a possible response
}

// handle processes a request from a client.
//
// It either writes a response in the request.response channel or an error in
// the request.errc channel.
func handle(r *request) {
	fields := strings.Fields(r.request)
	if len(fields) == 0 {
		r.errc <- errors.New("empty request")
		return
	}

	command := strings.ToLower(fields[0])

	handler, ok := commandHandlers[command]
	if !ok {
		r.errc <- fmt.Errorf("unknown command '%s'", command)
		return
	}

	response, err := handler(fields[1:])
	if err != nil {
		r.errc <- err
		return
	}

	r.response <- response
	close(r.response)
}

// serve processes requests written in the queue channel and quits when the
// done channel is closed.
func serve(queue <-chan *request, done <-chan struct{}) {
	for {
		select {
		case req := <-queue:
			handle(req)
		case <-done:
			return
		}
	}
}

// acquire reads a command from a client, writes the request to the queue
// channel and waits for a response or an error.
//
// Only a valid response is sent back to the client, i.e. if there is an error
// the connection is just closed without a message.
func acquire(c net.Conn, queue chan *request) {
	defer c.Close()

	addr := c.RemoteAddr()
	log.Debug("connection from %s", addr)
	defer log.Debug("disconnection from %s", addr)

	reader := bufio.NewReader(c)
	req, err := reader.ReadString('\n')
	if err != nil {
		log.Error("reading from %s: %s", addr, err)
		return
	}

	req = strings.TrimSpace(req)
	log.Debug("request = %s", req)

	r := &request{
		request:  req,
		errc:     make(chan error, 1),
		response: make(chan string, 1),
	}

	queue <- r

	select {
	case err := <-r.errc:
		log.Error("handling request '%s' from %s: %s", req, addr, err)
		return
	case response := <-r.response:
		if response != "" {
			writer := bufio.NewWriter(c)
			writer.WriteString(response)
			writer.WriteString("\r\n")
			writer.Flush()
		}
	}
}

// usage of program.
func usage() {
	fmt.Fprintf(os.Stderr, "usage: sshproxy-managerd [config]\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	versionFlag := flag.Bool("version", false, "show version number and exit")
	flag.Usage = usage
	flag.Parse()

	if *versionFlag {
		fmt.Fprintf(os.Stderr, "sshproxy-managerd version %s\n", SSHPROXY_VERSION)
		os.Exit(0)
	}

	config_file := defaultConfig
	if flag.NArg() != 0 {
		config_file = flag.Arg(0)
	}

	if err := loadConfig(config_file); err != nil {
		log.Fatalf("ERROR: reading configuration '%s': %s", config_file, err)
	}

	logformat := "%{time:2006-01-02 15:04:05} %{level}: %{message}"
	syslogformat := "%{level}: %{message}"
	utils.MustSetupLogging("sshproxy-managerd", config.Log, logformat, syslogformat, config.Debug)

	log.Debug("config.debug = %v", config.Debug)
	log.Debug("config.listen = %s", config.Listen)
	log.Debug("config.log = %s", config.Log)
	log.Debug("config.check_interval = %s", config.Check_Interval.Duration())
	log.Debug("config.route_select = %s", config.Route_Select)
	log.Debug("config.routes = %v", config.Routes)
	log.Debug("config.groups = %v", config.Groups)
	log.Debug("config.users = %v", config.Users)

	l, err := net.Listen("tcp", config.Listen)
	if err != nil {
		log.Fatalf("error: listening: %s\n", err)
	}
	defer l.Close()

	log.Info("listening on %s\n", config.Listen)

	queue := make(chan *request)
	done := make(chan struct{})

	go serve(queue, done)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatalf("error: accepting connection: %s\n", err)
		}

		go acquire(conn, queue)
	}

	close(done)
}
