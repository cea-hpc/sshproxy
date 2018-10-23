// Copyright 2015-2017 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
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
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/pkg/term"
	"github.com/kr/pty"
)

// runCommand executes the *exec.Cmd command and waits for its completion,
// unless the context is cancelled in which case the command is killed.
//
// The command can already be started if the started boolean is true.
func runCommand(ctx context.Context, cmd *exec.Cmd, started bool) error {
	if !started {
		if err := cmd.Start(); err != nil {
			return err
		}
	}
	go cmd.Wait()

	for {
		select {
		case <-time.After(1 * time.Second):
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				if !cmd.ProcessState.Success() {
					return fmt.Errorf("unexpected exit: %s", cmd.ProcessState.String())
				}
				return nil
			}
		case <-ctx.Done():
			cmd.Process.Kill()
			return nil
		}
	}

	// not reached
}

// runStdCommand launches a command without the need for a PTY.
//
// The command will be stopped when the context is cancelled and the session
// recorded by rec.
func runStdCommand(ctx context.Context, cmd *exec.Cmd, rec *Recorder) error {
	cmd.Stdin = rec.Stdin
	cmd.Stdout = rec.Stdout
	cmd.Stderr = rec.Stderr
	return runCommand(ctx, cmd, false)
}

// runTtyCommand launches a command in a PTY.
//
// The command will be stopped when the context is cancelled and the session
// recorded by rec.
//
// From: https://github.com/9seconds/ah/blob/master/app/utils/exec.go
func runTtyCommand(ctx context.Context, cmd *exec.Cmd, rec *Recorder) error {
	commandStarted := false
	if rec != nil {
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
		commandStarted = true
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return runCommand(ctx, cmd, commandStarted)
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
			log.Debug("%s: %s", b.Prefix, l)
		}
	}
	return len(p), nil
}

// prepareBackgroundCommand returns an *exec.Cmd struct for the background
// command. It replaces the stdout and stderr with a BackgroundCommandLogger if
// debug is true.
func prepareBackgroundCommand(command string, debug bool) *exec.Cmd {
	args := strings.Fields(command)
	cmd := exec.Command(args[0], args[1:]...)

	if debug {
		stdout := &BackgroundCommandLogger{"bg_command.stdout"}
		stderr := &BackgroundCommandLogger{"bg_command.stderr"}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
	}

	return cmd
}
