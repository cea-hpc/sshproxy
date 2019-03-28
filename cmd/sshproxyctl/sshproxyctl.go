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
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/cea-hpc/sshproxy/pkg/etcd"
	"github.com/cea-hpc/sshproxy/pkg/utils"

	"github.com/olekukonko/tablewriter"
)

var (
	// SshproxyVersion is set by Makefile
	SshproxyVersion = "0.0.0+noproperlybuilt"
	defaultConfig   = "/etc/sshproxy/sshproxy.yaml"
	defaultHostPort = "22"
)

func bold(s string) string {
	return "\033[1m" + s + "\033[0m"
}

func mustInitEtcdClient(configFile string) *etcd.Client {
	config, err := utils.LoadConfig(configFile, "", "", time.Now(), nil)
	if err != nil {
		log.Fatalf("reading configuration file %s: %v", configFile, err)
	}

	cli, err := etcd.NewClient(config, nil)
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
	User string
	Host string
	Port string
	Dest string
	N    int
	Last time.Time
}

type aggregatedConnections []*aggConnection

func (ac aggregatedConnections) toRows() [][]string {
	rows := make([][]string, len(ac))

	for i, c := range ac {
		rows[i] = []string{
			c.User,
			c.Host,
			c.Port,
			c.Dest,
			strconv.Itoa(c.N),
			c.Last.Format("2006-01-02 15:04:05"),
		}
	}

	return rows
}

type flatConnections []*etcd.FlatConnection

func (fc flatConnections) getAllConnections() [][]string {
	rows := make([][]string, len(fc))

	for i, c := range fc {
		rows[i] = []string{
			c.User,
			c.Host,
			c.Port,
			c.Dest,
			c.Ts.Format("2006-01-02 15:04:05"),
		}
	}

	return rows
}

func (fc flatConnections) getAggregatedConnections() aggregatedConnections {
	type conn struct {
		User string
		Host string
		Port string
		Dest string
	}

	type connInfo struct {
		N  int
		Ts time.Time
	}

	conns := make(map[conn]*connInfo)

	for _, c := range fc {
		key := conn{User: c.User, Host: c.Host, Port: c.Port, Dest: c.Dest}

		if val, present := conns[key]; present {
			val.N++
			val.Ts = c.Ts
		} else {
			conns[key] = &connInfo{
				N:  1,
				Ts: c.Ts,
			}
		}
	}

	var connections aggregatedConnections

	for k, v := range conns {
		connections = append(connections, &aggConnection{
			k.User,
			k.Host,
			k.Port,
			k.Dest,
			v.N,
			v.Ts,
		})
	}

	sort.Slice(connections, func(i, j int) bool {
		switch {
		case connections[i].User != connections[j].User:
			return connections[i].User < connections[j].User
		case connections[i].Host != connections[j].Host:
			return connections[i].Host < connections[j].Host
		case connections[i].Port != connections[j].Port:
			return connections[i].Port < connections[j].Port
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
		rows = fc.getAllConnections()
	} else {
		rows = fc.getAggregatedConnections().toRows()
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
		rows = fc.getAllConnections()
	} else {
		rows = fc.getAggregatedConnections().toRows()
	}

	var headers []string
	if allFlag {
		headers = []string{"User", "From", "Port", "Destination", "Start time"}
	} else {
		headers = []string{"User", "From", "Port", "Destination", "# of connections", "Last connection"}
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
		}
	}

	if csvFlag {
		displayCSV(rows)
	} else {
		displayTable([]string{"Host", "Port", "State", "Last check"}, rows)
	}
}

func enableHost(host, port, configFile string) error {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.SetHost(key, etcd.Up, time.Now())
}

func disableHost(host, port, configFile string) error {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.SetHost(key, etcd.Disabled, time.Now())
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
	fs.BoolVar(allFlag, "all", false, "show all connections")
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: %s show [OPTIONS] COMMAND

The commands are:
  connections   show connections stored in etcd
  hosts         show hosts stored in etcd

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
		switch subcmd {
		case "hosts":
			showHosts(*configFile, csvFlag, jsonFlag)
		case "connections":
			showConnections(*configFile, csvFlag, jsonFlag, allFlag)
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
