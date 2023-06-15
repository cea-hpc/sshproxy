// Copyright 2015-2023 CEA/DAM/DIF
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
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/docker/docker/pkg/term"
	"github.com/kr/pty"
)

// runCommand executes the *exec.Cmd command and waits for its completion.
//
// The command can already be started if the started boolean is true.
//
// Returns the exit code of the command or an error.
func runCommand(cmd *exec.Cmd, started bool) (int, error) {
	if !started {
		if err := cmd.Start(); err != nil {
			return -1, err
		}
	}

	err := cmd.Wait()
	rc := cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
	return rc, err
}

// runStdCommand launches a command without the need for a PTY.
//
// Returns the exit code of the command or an error.
func runStdCommand(cmd *exec.Cmd, rec *Recorder) (int, error) {
	if rec != nil {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return -1, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return -1, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return -1, err
		}
		go func() {
			io.Copy(stdin, rec.Stdin)
			stdin.Close() // release stdin when rec.Stdin is closed
		}()
		go io.Copy(rec.Stdout, stdout)
		go io.Copy(rec.Stderr, stderr)
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return runCommand(cmd, false)
}

// runTtyCommand launches a command in a PTY.
//
// From: https://github.com/9seconds/ah/blob/master/app/utils/exec.go
//
// Returns the exit code of the command or an error.
func runTtyCommand(cmd *exec.Cmd, rec *Recorder) (int, error) {
	commandStarted := false
	if rec != nil {
		p, err := pty.Start(cmd)
		if err != nil {
			return -1, err
		}
		defer p.Close()

		hostFd := os.Stdin.Fd()
		oldState, err := term.SetRawTerminal(hostFd)
		if err != nil {
			return -1, err
		}
		defer term.RestoreTerminal(hostFd, oldState)

		monitorTtyResize(hostFd, p.Fd())

		go io.Copy(p, rec.Stdin)
		go io.Copy(rec.Stdout, p)
		commandStarted = true
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return runCommand(cmd, commandStarted)
}

// monitorTtyResize resizes the guestFd TTY to the hostFd TTY size and checks
// if the hostFd TTY is resized.
//
// It is needed by runTtyCommand.
func monitorTtyResize(hostFd uintptr, guestFd uintptr) {
	resizeTty(hostFd, guestFd)

	winchChan := make(chan os.Signal, 1)
	signal.Notify(winchChan, syscall.SIGWINCH)

	go func() {
		for range winchChan {
			resizeTty(hostFd, guestFd)
		}
	}()
}

// resizeTty resizes the guestFd TTY to the hostFd TTY size.
//
// It is needed by monitorTtyResize.
func resizeTty(hostFd uintptr, guestFd uintptr) {
	winsize, err := term.GetWinsize(hostFd)
	if err != nil {
		return
	}
	term.SetWinsize(guestFd, winsize)
}

// A BackgroundCommandLogger is a special logger designed to log background
// application stdout/stderr only when debug is enabled.
type BackgroundCommandLogger struct {
	Prefix string
}

// Write implements the Writer Write method. It logs each non-empty line in
// a separated log line.
func (b *BackgroundCommandLogger) Write(p []byte) (int, error) {
	lines := strings.Split(bytes.NewBuffer(p).String(), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) != 0 {
			log.Debugf("%s: %s", b.Prefix, l)
		}
	}
	return len(p), nil
}

// prepareBackgroundCommand returns an *exec.Cmd struct for the background
// command. It replaces the stdout and stderr with a BackgroundCommandLogger if
// debug is true.
func prepareBackgroundCommand(ctx context.Context, command string, debug bool) *exec.Cmd {
	args := strings.Fields(command)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	if debug {
		stdout := &BackgroundCommandLogger{"bg_command.stdout"}
		stderr := &BackgroundCommandLogger{"bg_command.stderr"}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
	}

	return cmd
}
