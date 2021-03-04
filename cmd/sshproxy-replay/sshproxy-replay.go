// Copyright 2015-2021 CEA/DAM/DIF
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
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/cea-hpc/sshproxy/pkg/record"
)

var (
	// SshproxyVersion is set in the Makefile.
	SshproxyVersion = "0.0.0+notproperlybuilt"
	replayFlag      = flag.Bool("replay", false, "live replay a session (as the user did it)")
	versionFlag     = flag.Bool("version", false, "show version number and exit")
)

func replay(filename string) {
	fmt.Printf("===> opening %s\n", filename)

	f, err := os.Open(filename)
	if err != nil {
		log.Printf("error reading: %s\n", err)
		return
	}
	defer f.Close()

	reader, err := record.NewReader(f)
	if err != nil {
		log.Printf("error: %s\n", err)
		return
	}

	fmt.Printf("--> Version: %d\n", reader.Info.Version)
	fmt.Printf("--> Start:   %s\n", reader.Info.Time)
	fmt.Printf("--> User:    %s\n", reader.Info.User)
	fmt.Printf("--> From:    %s\n", reader.Info.Src())
	fmt.Printf("--> To:      %s\n", reader.Info.Dst())
	fmt.Printf("--> Command: %s\n", reader.Info.Command)

	var rec record.Record
	var start, previous time.Time
	var elapsed, direction string
	var stream *os.File
	dayFormat := "Jan 02 15:04:05"
	for {
		err := reader.Next(&rec)
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("error reading: %s\n", err)
			return
		}
		if *replayFlag {
			if !previous.IsZero() {
				time.Sleep(rec.Time.Sub(previous))
			}
			previous = rec.Time
			switch rec.Fd {
			case 0:
				continue
			case 1:
				stream = os.Stdout
			case 2:
				stream = os.Stderr
			}
			stream.Write(rec.Data)
		} else {
			if start.IsZero() {
				start = rec.Time
				elapsed = rec.Time.Format(dayFormat)
			} else {
				elapsed = fmt.Sprintf("+%.6f", rec.Time.Sub(start).Seconds())
			}
			switch rec.Fd {
			case 0:
				direction = "-->"
			case 1:
				direction = "<--"
			case 2:
				direction = "<=="
			}
			fmt.Printf("[%[1]*s] [%s] %d bytes\n", len(dayFormat), elapsed, direction, rec.Size)
			fmt.Println(hex.Dump(rec.Data))
		}
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: sshproxy-replay files ...\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if *versionFlag {
		fmt.Fprintf(os.Stderr, "sshproxy-replay version %s\n", SshproxyVersion)
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		usage()
	}

	for _, fn := range flag.Args() {
		replay(fn)
	}
}
