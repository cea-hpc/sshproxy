// Copyright 2015-2020 CEA/DAM/DIF
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
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/cea-hpc/sshproxy/pkg/utils"

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
		headers = []string{"User", "Service", "From", "Destination", "Start time", "Bandwidth in", "Bandwidth out"}
	} else {
		headers = []string{"User", "Service", "Destination", "# of connections", "Last connection", "Bandwidth in", "Bandwidth out"}
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

func showUsers(configFile string, csvFlag bool, jsonFlag bool, allFlag bool) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	users, err := cli.GetAllUsers(allFlag)
	if err != nil {
		log.Fatalf("ERROR: getting users from etcd: %v", err)
	}

	if jsonFlag {
		displayJSON(users)
		return
	}

	rows := make([][]string, len(users))
	i := 0
	for k, v := range users {
		rows[i] = []string{
			k,
			v.Groups,
			fmt.Sprintf("%d", v.N),
			byteToHuman(v.BwIn, csvFlag),
			byteToHuman(v.BwOut, csvFlag),
		}
		i++
	}

	if csvFlag {
		displayCSV(rows)
	} else {
		displayTable([]string{"User", "Groups", "# of connections", "Bandwidth in", "Bandwidth out"}, rows)
	}
}

func showGroups(configFile string, csvFlag bool, jsonFlag bool, allFlag bool) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	groups, err := cli.GetAllGroups(allFlag)
	if err != nil {
		log.Fatalf("ERROR: getting groups from etcd: %v", err)
	}

	if jsonFlag {
		displayJSON(groups)
		return
	}

	rows := make([][]string, len(groups))
	i := 0
	for k, v := range groups {
		rows[i] = []string{
			k,
			v.Users,
			fmt.Sprintf("%d", v.N),
			byteToHuman(v.BwIn, csvFlag),
			byteToHuman(v.BwOut, csvFlag),
		}
		i++
	}

	if csvFlag {
		displayCSV(rows)
	} else {
		displayTable([]string{"Group", "Users", "# of connections", "Bandwidth in", "Bandwidth out"}, rows)
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
			h.Port,
			h.State.String(),
			h.Ts.Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%d", h.N),
			byteToHuman(h.BwIn, csvFlag),
			byteToHuman(h.BwOut, csvFlag),
		}
	}

	if csvFlag {
		displayCSV(rows)
	} else {
		displayTable([]string{"Host", "Port", "State", "Last check", "# of connections", "Bandwidth in", "Bandwidth out"}, rows)
	}
}

func enableHost(host, port, configFile string) error {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.SetHost(key, utils.Up, time.Now())
}

func disableHost(host, port, configFile string) error {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.SetHost(key, utils.Disabled, time.Now())
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
  disable       disable a host in etcd

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

func newShowParser(csvFlag *bool, jsonFlag *bool, allFlag *bool) *flag.FlagSet {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	fs.BoolVar(csvFlag, "csv", false, "show results in CSV format")
	fs.BoolVar(jsonFlag, "json", false, "show results in JSON format")
	fs.BoolVar(allFlag, "all", false, "show all connections / users / groups")
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s show [OPTIONS] COMMAND

The commands are:
  connections   show connections stored in etcd
  hosts         show hosts stored in etcd
  users         show users stored in etcd
  groups        show groups stored in etcd

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

Enable a previously disabled host in etcd. The default port is %s.
`, os.Args[0], defaultHostPort)
		os.Exit(2)
	}
	return fs
}

func newDisableParser() *flag.FlagSet {
	fs := flag.NewFlagSet("disable", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s disable HOST [PORT]

Disabe a host in etcd. The default port is %s.
`, os.Args[0], defaultHostPort)
		os.Exit(2)
	}
	return fs
}

func getHostPortFromCommandLine(args []string) (string, string, error) {
	host, port := "", "22"
	switch len(args) {
	case 2:
		host, port = args[0], args[1]
	case 1:
		host = args[0]
	default:
		return "", "", fmt.Errorf("wrong number of arguments")
	}
	return host, port, nil
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

	parsers := map[string]*flag.FlagSet{
		"help":    newHelpParser(),
		"version": newVersionParser(),
		"show":    newShowParser(&csvFlag, &jsonFlag, &allFlag),
		"enable":  newEnableParser(),
		"disable": newDisableParser(),
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
		default:
			fmt.Fprintf(os.Stderr, "ERROR: unknown subcommand: %s\n\n", subcmd)
			usage()
		}
	case "enable":
		p := parsers[cmd]
		p.Parse(args)
		host, port, err := getHostPortFromCommandLine(p.Args())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			usage()
		}
		enableHost(host, port, *configFile)
	case "disable":
		p := parsers[cmd]
		p.Parse(args)
		host, port, err := getHostPortFromCommandLine(p.Args())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			usage()
		}
		disableHost(host, port, *configFile)
	default:
		fmt.Fprintf(os.Stderr, "ERROR: unknown command: %s\n\n", cmd)
		usage()
	}
}
