// Copyright 2015-2025 CEA/DAM/DIF
//  Author: Arnaud Guignard <arnaud.guignard@cea.fr>
//  Contributor: Cyril Servant <cyril.servant@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package utils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/op/go-logging"
)

var MustSetupLoggingTests = []struct {
	debug, inputDebug bool
	input, want       string
}{
	{true, true, "test", "DEBUG test\n"},
	{false, false, "test", "INFO test\n"},
	{false, true, "test", ""},
}

func TestMustSetupLoggingStdout(t *testing.T) {
	logFormat := "%{level} %{message}"
	for _, tt := range MustSetupLoggingTests {
		// Save the original stdout
		originalStdout := os.Stdout

		// Create a new buffer and redirect stdout
		var buf bytes.Buffer
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("Failed to create pipe: %v", err)
		}
		os.Stdout = w

		var log = logging.MustGetLogger("sshproxy")
		MustSetupLogging("sshproxy", "", logFormat, logFormat, tt.debug)
		if tt.inputDebug {
			log.Debug(tt.input)
		} else {
			log.Info(tt.input)
		}

		// Stop writing and restore stdout
		w.Close()
		os.Stdout = originalStdout
		io.Copy(&buf, r)

		// Verify the output
		got := buf.String()
		if got != tt.want {
			t.Errorf("got: %s, want: %s", got, tt.want)
		}
	}
}

func TestMustSetupLoggingFile(t *testing.T) {
	logFormat := "%{level} %{message}"
	logFile := "/tmp/sshproxytest.log"
	os.Remove(logFile)
	for _, tt := range MustSetupLoggingTests {
		var log = logging.MustGetLogger("sshproxy")
		MustSetupLogging("sshproxy", logFile, logFormat, logFormat, tt.debug)

		// test first log
		if tt.inputDebug {
			log.Debug(tt.input)
		} else {
			log.Info(tt.input)
		}
		got, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		if string(got) != tt.want {
			t.Errorf("got: %s, want: %s", string(got), tt.want)
		}

		// test second log in same file
		if tt.inputDebug {
			log.Debug(tt.input)
		} else {
			log.Info(tt.input)
		}
		got, err = os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		if string(got) != tt.want+tt.want {
			t.Errorf("got: %s, want: %s", string(got), tt.want+tt.want)
		}

		os.Remove(logFile)
	}
}

func BenchmarkMustSetupLogging(b *testing.B) {
	logFormat := "%{level} %{message}"
	for _, logFile := range []string{"", "/tmp/sshproxytest.log"} {
		for _, debug := range []bool{true, false} {
			b.Run(fmt.Sprintf("%s_%v", logFile, debug), func(b *testing.B) {
				MustSetupLogging("sshproxy", logFile, logFormat, logFormat, debug)
			})
		}
	}
}
