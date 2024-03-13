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
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cea-hpc/sshproxy/pkg/utils"

	"github.com/iskylite/nodeset"
	"github.com/olekukonko/tablewriter"
)

var (
	// SshproxyVersion is set by Makefile
	SshproxyVersion = "0.0.0+noproperlybuilt"
	defaultConfig   = "/etc/sshproxy/sshproxy.yaml"
	defaultHostPort = "22"
)

func mustInitEtcdClient(configFile string) *utils.Client {
	config, err := utils.LoadConfig(configFile, "", "", time.Now(), nil)
	if err != nil {
		log.Fatalf("reading configuration file %s: %v", configFile, err)
	}

	cli, err := utils.NewEtcdClient(config, nil)
	if err != nil {
		log.Fatalf("configuring etcd client: %v", err)
	}

	return cli
}

func getErrorBanner(configFile string) string {
	config, err := utils.LoadConfig(configFile, "", "", time.Now(), nil)
	if err != nil {
		log.Fatalf("reading configuration file %s: %v", configFile, err)
	}

	return config.ErrorBanner
}

func displayCSV(rows [][]string) {
	w := csv.NewWriter(os.Stdout)
	w.WriteAll(rows)

	if err := w.Error(); err != nil {
		log.Fatalln("error writing csv:", err)
	}
}

func displayJSON(objs interface{}) {
	w := json.NewEncoder(os.Stdout)
	if err := w.Encode(&objs); err != nil {
		log.Fatalln("error writing JSON:", err)
	}
}

func displayTable(headers []string, rows [][]string) {
	table := tablewriter.NewWriter(os.Stdout)

	colours := make([]tablewriter.Colors, len(headers))
	for i := 0; i < len(headers); i++ {
		colours[i] = tablewriter.Colors{tablewriter.Bold}
	}

	table.SetHeader(headers)
	table.SetBorder(false)
	table.SetAutoFormatHeaders(false)
	//table.SetAutoWrapText(false)
	table.SetHeaderColor(colours...)
	table.AppendBulk(rows)
	table.Render()
}

type aggConnection struct {
	User    string
	Service string
	Dest    string
	N       int
	Last    time.Time
	BwIn    int
	BwOut   int
}

type aggregatedConnections []*aggConnection

func (ac aggregatedConnections) toRows(passthrough bool) [][]string {
	rows := make([][]string, len(ac))

	for i, c := range ac {
		rows[i] = []string{
			c.User,
			c.Service,
			c.Dest,
			strconv.Itoa(c.N),
			c.Last.Format("2006-01-02 15:04:05"),
			byteToHuman(c.BwIn, passthrough),
			byteToHuman(c.BwOut, passthrough),
		}
	}

	return rows
}

type flatConnections []*utils.FlatConnection

func (fc flatConnections) getAllConnections(passthrough bool) [][]string {
	rows := make([][]string, len(fc))

	for i, c := range fc {
		rows[i] = []string{
			c.User,
			c.Service,
			c.From,
			c.Dest,
			c.Ts.Format("2006-01-02 15:04:05"),
			byteToHuman(c.BwIn, passthrough),
			byteToHuman(c.BwOut, passthrough),
		}
	}

	return rows
}

func (fc flatConnections) getAggregatedConnections() aggregatedConnections {
	type conn struct {
		User    string
		Service string
		Dest    string
	}

	type connInfo struct {
		N     int
		Ts    time.Time
		BwIn  int
		BwOut int
	}

	conns := make(map[conn]*connInfo)

	for _, c := range fc {
		key := conn{User: c.User, Service: c.Service, Dest: c.Dest}

		if val, present := conns[key]; present {
			val.N++
			val.Ts = c.Ts
			val.BwIn += c.BwIn
			val.BwOut += c.BwOut
		} else {
			conns[key] = &connInfo{
				N:     1,
				Ts:    c.Ts,
				BwIn:  c.BwIn,
				BwOut: c.BwOut,
			}
		}
	}

	var connections aggregatedConnections

	for k, v := range conns {
		connections = append(connections, &aggConnection{
			k.User,
			k.Service,
			k.Dest,
			v.N,
			v.Ts,
			v.BwIn,
			v.BwOut,
		})
	}

	sort.Slice(connections, func(i, j int) bool {
		switch {
		case connections[i].User != connections[j].User:
			return connections[i].User < connections[j].User
		case connections[i].Service != connections[j].Service:
			return connections[i].Service < connections[j].Service
		case connections[i].Dest != connections[j].Dest:
			return connections[i].Dest < connections[j].Dest
		}
		return false
	})

	return connections
}

func (fc flatConnections) displayCSV(allFlag bool) {
	var rows [][]string

	if allFlag {
		rows = fc.getAllConnections(true)
	} else {
		rows = fc.getAggregatedConnections().toRows(true)
	}

	displayCSV(rows)
}

func (fc flatConnections) displayJSON(allFlag bool) {
	var objs interface{}

	if allFlag {
		objs = fc
	} else {
		objs = fc.getAggregatedConnections()
	}

	displayJSON(objs)
}

func (fc flatConnections) displayTable(allFlag bool) {
	var rows [][]string

	if allFlag {
		rows = fc.getAllConnections(false)
	} else {
		rows = fc.getAggregatedConnections().toRows(false)
	}

	var headers []string
	if allFlag {
		headers = []string{"User", "Service", "From", "Destination", "Start time", "Bw in", "Bw out"}
	} else {
		headers = []string{"User", "Service", "Destination", "# of conns", "Last connection", "Bw in", "Bw out"}
	}

	displayTable(headers, rows)
}

func showConnections(configFile string, csvFlag bool, jsonFlag bool, allFlag bool) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	var connections flatConnections
	connections, err := cli.GetAllConnections()
	if err != nil {
		log.Fatalf("ERROR: getting connections from etcd: %v", err)
	}

	if csvFlag {
		connections.displayCSV(allFlag)
	} else if jsonFlag {
		connections.displayJSON(allFlag)
	} else {
		connections.displayTable(allFlag)
	}
}

type flatUserLight struct {
	User   string
	Groups string
	N      int
	BwIn   int
	BwOut  int
}

type flatUsers []*utils.FlatUser

func (fu flatUsers) getAllUsers(allFlag bool, passthrough bool) [][]string {
	rows := make([][]string, len(fu))
	for i, v := range fu {
		if allFlag {
			rows[i] = []string{
				v.User,
				v.Service,
				v.Groups,
				fmt.Sprintf("%d", v.N),
				byteToHuman(v.BwIn, passthrough),
				byteToHuman(v.BwOut, passthrough),
				v.Dest,
				secondsToHuman(v.TTL, passthrough),
			}
		} else {
			rows[i] = []string{
				v.User,
				v.Groups,
				fmt.Sprintf("%d", v.N),
				byteToHuman(v.BwIn, passthrough),
				byteToHuman(v.BwOut, passthrough),
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if allFlag && rows[i][0] == rows[j][0] {
			return rows[i][1] < rows[j][1]
		} else {
			return rows[i][0] < rows[j][0]
		}
	})

	return rows
}

func (fu flatUsers) displayJSON(allFlag bool) {
	if allFlag {
		displayJSON(fu)
	} else {
		users := make([]*flatUserLight, len(fu))
		for i, v := range fu {
			users[i] = &flatUserLight{
				v.User,
				v.Groups,
				v.N,
				v.BwIn,
				v.BwOut,
			}
		}
		displayJSON(users)
	}
}

func (fu flatUsers) displayCSV(allFlag bool) {
	rows := fu.getAllUsers(allFlag, true)

	displayCSV(rows)
}

func (fu flatUsers) displayTable(allFlag bool) {
	rows := fu.getAllUsers(allFlag, false)

	var headers []string
	if allFlag {
		headers = []string{"User", "Service", "Groups", "# of conns", "Bw in", "Bw out", "Persist to", "Persist TTL"}
	} else {
		headers = []string{"User", "Groups", "# of conns", "Bw in", "Bw out"}
	}

	displayTable(headers, rows)
}

func showUsers(configFile string, csvFlag bool, jsonFlag bool, allFlag bool) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	var users flatUsers
	users, err := cli.GetAllUsers(allFlag)
	if err != nil {
		log.Fatalf("ERROR: getting users from etcd: %v", err)
	}

	if jsonFlag {
		users.displayJSON(allFlag)
	} else if csvFlag {
		users.displayCSV(allFlag)
	} else {
		users.displayTable(allFlag)
	}
}

type flatGroupLight struct {
	Group string
	Users string
	N     int
	BwIn  int
	BwOut int
}

type flatGroups []*utils.FlatGroup

func (fg flatGroups) getAllGroups(allFlag bool, passthrough bool) [][]string {
	rows := make([][]string, len(fg))
	for i, v := range fg {
		if allFlag {
			rows[i] = []string{
				v.Group,
				v.Service,
				v.Users,
				fmt.Sprintf("%d", v.N),
				byteToHuman(v.BwIn, passthrough),
				byteToHuman(v.BwOut, passthrough),
			}
		} else {
			rows[i] = []string{
				v.Group,
				v.Users,
				fmt.Sprintf("%d", v.N),
				byteToHuman(v.BwIn, passthrough),
				byteToHuman(v.BwOut, passthrough),
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	return rows
}

func (fg flatGroups) displayJSON(allFlag bool) {
	if allFlag {
		displayJSON(fg)
	} else {
		groups := make([]*flatGroupLight, len(fg))
		for i, v := range fg {
			groups[i] = &flatGroupLight{
				v.Group,
				v.Users,
				v.N,
				v.BwIn,
				v.BwOut,
			}
		}
		displayJSON(groups)
	}
}

func (fg flatGroups) displayCSV(allFlag bool) {
	rows := fg.getAllGroups(allFlag, true)

	displayCSV(rows)
}

func (fg flatGroups) displayTable(allFlag bool) {
	rows := fg.getAllGroups(allFlag, false)

	var headers []string
	if allFlag {
		headers = []string{"Group", "Service", "Users", "# of conns", "Bw in", "Bw out"}
	} else {
		headers = []string{"Group", "Users", "# of conns", "Bw in", "Bw out"}
	}

	displayTable(headers, rows)
}

func showGroups(configFile string, csvFlag bool, jsonFlag bool, allFlag bool) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	var groups flatGroups
	groups, err := cli.GetAllGroups(allFlag)
	if err != nil {
		log.Fatalf("ERROR: getting groups from etcd: %v", err)
	}

	if jsonFlag {
		groups.displayJSON(allFlag)
	} else if csvFlag {
		groups.displayCSV(allFlag)
	} else {
		groups.displayTable(allFlag)
	}
}

func showHosts(configFile string, csvFlag bool, jsonFlag bool) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	hosts, err := cli.GetAllHosts()
	if err != nil {
		log.Fatalf("ERROR: getting hosts from etcd: %v", err)
	}

	if jsonFlag {
		displayJSON(hosts)
		return
	}

	rows := make([][]string, len(hosts))

	for i, h := range hosts {
		rows[i] = []string{
			h.Hostname,
			h.State.String(),
			h.Ts.Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%d", h.N),
			byteToHuman(h.BwIn, csvFlag),
			byteToHuman(h.BwOut, csvFlag),
			fmt.Sprintf("%d", h.HistoryN),
		}
	}

	if csvFlag {
		displayCSV(rows)
	} else {
		displayTable([]string{"Host", "State", "Last check", "# of conns", "Bw in", "Bw out", "# persist"}, rows)
	}
}

func enableHost(host, port, configFile string) error {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.SetHost(key, utils.Up, time.Now())
}

func forgetHost(host, port, configFile string) error {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.DelHost(key)
}

func disableHost(host, port, configFile string) error {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.SetHost(key, utils.Disabled, time.Now())
}

func setErrorBanner(errorBanner string, expire time.Time, configFile string) error {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	if errorBanner == "" {
		return cli.DelErrorBanner()
	}
	return cli.SetErrorBanner(errorBanner, expire)
}

func showErrorBanner(configFile string) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()
	errorBanner, expire, err := cli.GetErrorBanner()
	if err != nil {
		log.Fatalf("ERROR: getting error banner from etcd: %v", err)
	}

	fmt.Fprintf(flag.CommandLine.Output(), "Default error banner:\n%s\n", getErrorBanner(configFile))
	if errorBanner != "" {
		if expire == "" {
			expire = "never"
		}
		fmt.Fprintf(flag.CommandLine.Output(), "Current error banner (expiration date: %s):\n%s\n", expire, errorBanner)
	}
}

func showConfig(configFile, userString, groupsString string) {
	groupsMap := make(map[string]bool)
	userComment := ""
	// get system groups of given user, if it exists
	userObject, err := user.Lookup(userString)
	if err != nil {
		userComment = " (unknown on this system)"
	} else {
		groupsMap, _ = utils.GetGroupUser(userObject)
	}
	// add given groups to system groups
	for _, group := range strings.Split(groupsString, ",") {
		if group != "" {
			groupsMap[group] = true
		}
	}
	// get config for given user / groups
	config, err := utils.LoadConfig(configFile, userString, "", time.Now(), groupsMap)
	if err != nil {
		log.Fatalf("reading configuration file %s: %v", configFile, err)
	}
	fmt.Fprintf(os.Stdout, "user = %s%s\n", userString, userComment)
	for _, configLine := range utils.PrintConfig(config, groupsMap) {
		fmt.Fprintln(os.Stdout, configLine)
	}
}

func showVersion() {
	fmt.Fprintf(flag.CommandLine.Output(), "%s version %s\n", os.Args[0], SshproxyVersion)
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s [OPTIONS] COMMAND

The commands are:
  help          display help on a command
  version       show version number and exit
  show          show states present in etcd
  enable        enable a host in etcd
  forget        forget a host in etcd
  disable       disable a host in etcd
  error_banner  set the error banner in etcd

The common options are:
`, os.Args[0])
	flag.PrintDefaults()
	os.Exit(2)
}

func newHelpParser() *flag.FlagSet {
	fs := flag.NewFlagSet("help", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s help COMMAND

Show help of a command.
`, os.Args[0])
		os.Exit(2)
	}
	return fs
}

func newVersionParser() *flag.FlagSet {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s version

Show version and exit.
`, os.Args[0])
		os.Exit(2)
	}
	return fs
}

func newShowParser(csvFlag *bool, jsonFlag *bool, allFlag *bool, userString *string, groupsString *string) *flag.FlagSet {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	fs.BoolVar(csvFlag, "csv", false, "show results in CSV format")
	fs.BoolVar(jsonFlag, "json", false, "show results in JSON format")
	fs.BoolVar(allFlag, "all", false, "show all connections / users / groups")
	fs.StringVar(userString, "user", "", "show the config for this specific user and this user's groups (if any)")
	fs.StringVar(groupsString, "groups", "", "show the config for these specific groups (comma separated)")
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s show COMMAND [OPTIONS]

The commands are:
  connections [-all] [-csv|-json]       show connections stored in etcd
  hosts [-csv|-json]                    show hosts stored in etcd
  users [-all] [-csv|-json]             show users stored in etcd
  groups [-all] [-csv|-json]            show groups stored in etcd
  error_banner                          show error banners stored in etcd and in configuration
  config [-user USER] [-groups GROUPS]  show the calculated configuration

The options are:
`, os.Args[0])
		fs.PrintDefaults()
		os.Exit(2)
	}

	return fs
}

func newEnableParser() *flag.FlagSet {
	fs := flag.NewFlagSet("enable", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s enable HOST [PORT]

Enable a previously disabled host in etcd. The default port is %s. Host and port
can be nodesets.
`, os.Args[0], defaultHostPort)
		os.Exit(2)
	}
	return fs
}

func newForgetParser() *flag.FlagSet {
	fs := flag.NewFlagSet("forget", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s forget HOST [PORT]

Forget a host in etcd. The default port is %s. Remember that if this host is
used, it will appear back in the list. Host and port can be nodesets.
`, os.Args[0], defaultHostPort)
		os.Exit(2)
	}
	return fs
}

func newDisableParser() *flag.FlagSet {
	fs := flag.NewFlagSet("disable", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s disable HOST [PORT]

Disabe a host in etcd. The default port is %s. Host and port can be nodesets.
`, os.Args[0], defaultHostPort)
		os.Exit(2)
	}
	return fs
}

func newErrorBannerParser(expireFlag *string) *flag.FlagSet {
	fs := flag.NewFlagSet("error_banner", flag.ExitOnError)
	fs.StringVar(expireFlag, "expire", "", "set the expiration date of this error banner. Format: YYYY-MM-DD[ HH:MM[:SS]]")
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s error_banner [-expire DATE] MESSAGE

Set the error banner in etcd.

The options are:
`, os.Args[0])
		fs.PrintDefaults()
		os.Exit(2)
	}
	return fs
}

func getHostPortFromCommandLine(args []string) ([]string, []string, error) {
	hostsNodeset, portsNodeset := "", defaultHostPort
	switch len(args) {
	case 2:
		hostsNodeset, portsNodeset = args[0], args[1]
	case 1:
		hostsNodeset = args[0]
	default:
		return []string{}, []string{}, fmt.Errorf("wrong number of arguments")
	}
	hosts, err := nodeset.Expand(hostsNodeset)
	if err != nil {
		return []string{}, []string{}, fmt.Errorf("%s", err)
	}
	ports, err := nodeset.Expand(portsNodeset)
	if err != nil {
		return []string{}, []string{}, fmt.Errorf("%s", err)
	}
	for _, port := range ports {
		if iport, err := strconv.Atoi(port); err != nil {
			return []string{}, []string{}, fmt.Errorf("port \"%s\" must be an integer", port)
		} else if iport < 0 || iport > 65535 {
			return []string{}, []string{}, fmt.Errorf("port \"%s\" must be in the 0-65535 range", port)
		}
		for _, host := range hosts {
			if _, _, err := net.SplitHostPort(host + ":" + port); err != nil {
				return []string{}, []string{}, fmt.Errorf("%s", err)
			}
		}
	}
	return hosts, ports, nil
}

func getErrorBannerFromCommandLine(args []string) (string, error) {
	errorBanner := ""
	switch len(args) {
	case 0:
		errorBanner = ""
	case 1:
		errorBanner = args[0]
	default:
		return "", fmt.Errorf("wrong number of arguments")
	}
	return errorBanner, nil
}

func byteToHuman(b int, passthrough bool) string {
	if passthrough {
		return fmt.Sprintf("%d", b)
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d kB/s", b)
	}
	div, exp := unit, 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB/s", float32(b)/float32(div), "MGT"[exp])
}

func secondsToHuman(s int64, passthrough bool) string {
	seconds := float64(s)
	if seconds == 0 {
		return ""
	} else if passthrough {
		return fmt.Sprintf("%.f", seconds)
	} else if seconds < 60 {
		return fmt.Sprintf("%.fs", seconds)
	}
	m := math.Floor(seconds / 60)
	seconds -= m * 60
	if m < 60 {
		return fmt.Sprintf("%.fm %.fs", m, seconds)
	}
	h := math.Floor(m / 60)
	m -= h * 60
	if h < 24 {
		return fmt.Sprintf("%.fh %.fm %.fs", h, m, seconds)
	}
	d := math.Floor(h / 24)
	h -= d * 24
	return fmt.Sprintf("%.fd %.fh %.fm %.fs", d, h, m, seconds)
}

func matchExpire(expire string) (time.Time, error) {
	layouts := []string{"2006-01-02", "2006-01-02 15:04", "2006-01-02 15:04:05"}
	loc, _ := time.LoadLocation("Local")
	var err error
	var t time.Time
	for _, layout := range layouts {
		t, err = time.ParseInLocation(layout, expire, loc)
		if err == nil {
			return t, nil
		}
	}
	if expire != "" {
		return t, err
	}
	return t, nil
}

func main() {
	flag.Usage = usage
	configFile := flag.String("c", defaultConfig, "path to configuration file")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: missing command\n\n")
		usage()
	}

	var csvFlag bool
	var jsonFlag bool
	var allFlag bool
	var expire string
	var userString string
	var groupsString string

	parsers := map[string]*flag.FlagSet{
		"help":         newHelpParser(),
		"version":      newVersionParser(),
		"show":         newShowParser(&csvFlag, &jsonFlag, &allFlag, &userString, &groupsString),
		"enable":       newEnableParser(),
		"forget":       newForgetParser(),
		"disable":      newDisableParser(),
		"error_banner": newErrorBannerParser(&expire),
	}

	cmd := flag.Arg(0)
	args := flag.Args()[1:]
	switch cmd {
	case "help":
		p := parsers[cmd]
		p.Parse(args)
		if p.NArg() == 0 {
			usage()
		}
		subcmd := p.Arg(0)
		if p2, present := parsers[subcmd]; present {
			p2.Usage()
		} else {
			fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", subcmd)
			usage()
		}
	case "version":
		p := parsers[cmd]
		p.Parse(args)
		showVersion()
	case "show":
		p := parsers[cmd]
		p.Parse(args)
		if p.NArg() == 0 {
			fmt.Fprintf(os.Stderr, "ERROR: missing 'hosts' or 'connections'\n\n")
			p.Usage()
		}
		subcmd := p.Arg(0)
		// parse flags after subcommand
		args = p.Args()[1:]
		p.Parse(args)
		switch subcmd {
		case "hosts":
			showHosts(*configFile, csvFlag, jsonFlag)
		case "connections":
			showConnections(*configFile, csvFlag, jsonFlag, allFlag)
		case "users":
			showUsers(*configFile, csvFlag, jsonFlag, allFlag)
		case "groups":
			showGroups(*configFile, csvFlag, jsonFlag, allFlag)
		case "error_banner":
			showErrorBanner(*configFile)
		case "config":
			showConfig(*configFile, userString, groupsString)
		default:
			fmt.Fprintf(os.Stderr, "ERROR: unknown subcommand: %s\n\n", subcmd)
			p.Usage()
		}
	case "enable":
		p := parsers[cmd]
		p.Parse(args)
		hosts, ports, err := getHostPortFromCommandLine(p.Args())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			p.Usage()
		}
		for _, host := range hosts {
			for _, port := range ports {
				enableHost(host, port, *configFile)
			}
		}
	case "forget":
		p := parsers[cmd]
		p.Parse(args)
		hosts, ports, err := getHostPortFromCommandLine(p.Args())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			p.Usage()
		}
		for _, host := range hosts {
			for _, port := range ports {
				forgetHost(host, port, *configFile)
			}
		}
	case "disable":
		p := parsers[cmd]
		p.Parse(args)
		hosts, ports, err := getHostPortFromCommandLine(p.Args())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			p.Usage()
		}
		for _, host := range hosts {
			for _, port := range ports {
				disableHost(host, port, *configFile)
			}
		}
	case "error_banner":
		p := parsers[cmd]
		p.Parse(args)
		errorBanner, err := getErrorBannerFromCommandLine(p.Args())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			p.Usage()
		}
		t, err := matchExpire(expire)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			p.Usage()
		}
		if expire != "" && t.Before(time.Now()) {
			fmt.Fprintf(os.Stderr, "ERROR: %s is in the past!\n\n", expire)
			p.Usage()
		}
		setErrorBanner(errorBanner, t, *configFile)
	default:
		fmt.Fprintf(os.Stderr, "ERROR: unknown command: %s\n\n", cmd)
		usage()
	}
}
