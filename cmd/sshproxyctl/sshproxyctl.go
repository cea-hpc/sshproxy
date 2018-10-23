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

	// flags
	versionFlag bool
	configFile  string
)

func bold(s string) string {
	return "\033[1m" + s + "\033[0m"
}

func mustInitEtcdClient() *etcd.Client {
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

func showConnections() {
	cli := mustInitEtcdClient()
	defer cli.Close()

	connections, err := cli.GetAllConnections()
	if err != nil {
		log.Fatalf("ERROR: getting connections from etcd: %v", err)
	}

	rows := make([][]string, len(connections))

	for _, c := range connections {
		rows = append(rows, []string{
			c.User,
			c.Host,
			c.Port,
			c.Dest,
			strconv.Itoa(c.N),
			c.Ts.Format("2006-01-02 15:04:05"),
		})
	}

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

func showHosts() {
	cli := mustInitEtcdClient()
	defer cli.Close()

	hosts, err := cli.GetAllHosts()
	if err != nil {
		log.Fatalf("ERROR: getting hosts from etcd: %v", err)
	}

	rows := make([][]string, len(hosts))

	for _, h := range hosts {
		rows = append(rows, []string{
			h.Hostname,
			h.Port,
			h.State.String(),
			h.Ts.Format("2006-01-02 15:04:05"),
		})
	}

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

func enableHost(host, port string) error {
	cli := mustInitEtcdClient()
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.SetHost(key, etcd.Up, time.Now())
}

func disableHost(host, port string) error {
	cli := mustInitEtcdClient()
	defer cli.Close()

	key := fmt.Sprintf("%s:%s", host, port)
	return cli.SetHost(key, etcd.Disabled, time.Now())
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: sshproxyctl [OPTIONS] COMMAND

The commands are:
  version
	show version number and exit
  show hosts
  	show hosts
  show connections
	show connections
  enable HOST [PORT]
  	enable a disabled host in etcd (default port is 22)
  disable HOST [PORT]
	disable a host in etcd (default port is 22)

The options are:
`)
	flag.PrintDefaults()
	os.Exit(2)
}

func showVersion() {
	fmt.Fprintf(os.Stderr, "sshproxyctl version %s\n", SshproxyVersion)
	os.Exit(0)
}

func init() {
	flag.BoolVar(&versionFlag, "V", false, "show version number and exit")
	flag.StringVar(&configFile, "c", defaultConfig, "path to configuration file")
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
	flag.Parse()

	if versionFlag {
		showVersion()
	}

	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: missing command\n\n")
		usage()
	}

	switch flag.Arg(0) {
	case "version":
		showVersion()
	case "show":
		if flag.NArg() != 2 {
			fmt.Fprintf(os.Stderr, "ERROR: missing 'hosts' or 'connections'\n\n")
			os.Exit(2)
		}
		switch flag.Arg(1) {
		case "hosts":
			showHosts()
		case "connections":
			showConnections()
		default:
			fmt.Fprintf(os.Stderr, "ERROR: unknown keyword '%s'\n\n", flag.Arg(1))
			usage()
		}
	case "enable":
		host, port, err := getHostPortFromCommandLine(flag.Args()[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			usage()
		}
		enableHost(host, port)
	case "disable":
		host, port, err := getHostPortFromCommandLine(flag.Args()[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err)
			usage()
		}
		disableHost(host, port)
	case "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "ERROR: unknown command '%s'\n\n", flag.Arg(0))
		usage()
	}
}
