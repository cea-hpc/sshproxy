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
	"flag"
	"fmt"
	"log"
	"os"
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

func showConnections(configFile string, csvFlag bool) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	connections, err := cli.GetAllConnections()
	if err != nil {
		log.Fatalf("ERROR: getting connections from etcd: %v", err)
	}

	rows := make([][]string, len(connections))

	for i, c := range connections {
		rows[i] = []string{
			c.User,
			c.Host,
			c.Port,
			c.Dest,
			strconv.Itoa(c.N),
			c.Ts.Format("2006-01-02 15:04:05"),
		}
	}

	if csvFlag {
		w := csv.NewWriter(os.Stdout)
		w.WriteAll(rows)

		if err := w.Error(); err != nil {
			log.Fatalln("error writing csv:", err)
		}
	} else {
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"User", "From", "Port", "Destination", "# of connections", "Last connection"})
		table.SetBorder(false)
		table.SetAutoFormatHeaders(false)
		//table.SetAutoWrapText(false)
		table.SetHeaderColor(
			tablewriter.Colors{tablewriter.Bold},
			tablewriter.Colors{tablewriter.Bold},
			tablewriter.Colors{tablewriter.Bold},
			tablewriter.Colors{tablewriter.Bold},
			tablewriter.Colors{tablewriter.Bold},
			tablewriter.Colors{tablewriter.Bold},
		)
		table.AppendBulk(rows)
		table.Render()
	}
}

func showHosts(configFile string, csvFlag bool) {
	cli := mustInitEtcdClient(configFile)
	defer cli.Close()

	hosts, err := cli.GetAllHosts()
	if err != nil {
		log.Fatalf("ERROR: getting hosts from etcd: %v", err)
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
		w := csv.NewWriter(os.Stdout)
		w.WriteAll(rows)

		if err := w.Error(); err != nil {
			log.Fatalln("error writing csv:", err)
		}
	} else {
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Host", "Port", "State", "Last check"})
		table.SetBorder(false)
		table.SetAutoFormatHeaders(false)
		//table.SetAutoWrapText(false)
		table.SetHeaderColor(
			tablewriter.Colors{tablewriter.Bold},
			tablewriter.Colors{tablewriter.Bold},
			tablewriter.Colors{tablewriter.Bold},
			tablewriter.Colors{tablewriter.Bold},
		)
		table.AppendBulk(rows)
		table.Render()
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

func newShowParser(csvFlag *bool) *flag.FlagSet {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	fs.BoolVar(csvFlag, "csv", false, "show results in a CSV format to parse it easily")
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

	parsers := map[string]*flag.FlagSet{
		"help":    newHelpParser(),
		"version": newVersionParser(),
		"show":    newShowParser(&csvFlag),
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
		if p2, ok := parsers[subcmd]; ok {
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
			showHosts(*configFile, csvFlag)
		case "connections":
			showConnections(*configFile, csvFlag)
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
