package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/op/go-logging"
)

var (
	defaultConfig = "/etc/sshproxy.cfg"

	defaultSshExe  = "ssh"
	defaultSshArgs = []string{"-q", "-Y"}
)

var log = logging.MustGetLogger("sshproxy")

type sshProxyConfig struct {
	Debug      bool
	Log        string
	Bg_Command string
	Ssh        sshConfig
	Users      map[string]userConfig
}

type sshConfig struct {
	Exe         string
	Destination string
	Args        []string
}

type userConfig struct {
	Debug      bool
	Log        string
	Bg_Command string
	Ssh        sshConfig
}

func MustSetupLogging(template, current_user, source string, debug bool) {
	var logBackend logging.Backend
	logFormat := fmt.Sprintf("%%{time:2006-01-02 15:04:05} %%{level} [%s] %%{message}", source)
	if template == "syslog" {
		var err error
		logBackend, err = logging.NewSyslogBackend("sshproxy")
		if err != nil {
			log.Fatalf("error opening syslog: %s", err)
		}
		logFormat = fmt.Sprintf("%%{level} [%s@%s] %%{message}", current_user, source)
	} else {
		var f *os.File
		if template == "" {
			f = os.Stderr
		} else {
			var err error
			fn := regexp.MustCompile(`{user}`).ReplaceAllString(template, current_user)
			f, err = os.OpenFile(fn, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				log.Fatalf("error opening log file %s: %v", fn, err)
			}
		}
		logBackend = logging.NewLogBackend(f, "", 0)
	}

	logging.SetBackend(logBackend)
	logging.SetFormatter(logging.MustStringFormatter(logFormat))
	if debug {
		logging.SetLevel(logging.DEBUG, "sshproxy")
	} else {
		logging.SetLevel(logging.NOTICE, "sshproxy")
	}
}

func LoadConfig(config_file, username string) (*sshProxyConfig, error) {
	var config sshProxyConfig
	md, err := toml.DecodeFile(config_file, &config)
	if err != nil {
		return nil, err
	}

	if !md.IsDefined("ssh", "destination") {
		return nil, fmt.Errorf("no ssh.destination specified")
	}

	if !md.IsDefined("ssh", "exe") {
		config.Ssh.Exe = defaultSshExe
	}

	if !md.IsDefined("ssh", "args") {
		config.Ssh.Args = defaultSshArgs
	}

	if userconfig, present := config.Users[username]; present {
		if md.IsDefined("users", username, "debug") {
			config.Debug = userconfig.Debug
		}

		if md.IsDefined("users", username, "log") {
			config.Log = userconfig.Log
		}

		if md.IsDefined("users", username, "bg_command") {
			config.Bg_Command = userconfig.Bg_Command
		}

		if md.IsDefined("users", username, "ssh", "exe") {
			config.Ssh.Exe = userconfig.Ssh.Exe
		}

		if md.IsDefined("users", username, "ssh", "destination") {
			config.Ssh.Destination = userconfig.Ssh.Destination
		}

		if md.IsDefined("users", username, "ssh", "args") {
			config.Ssh.Args = userconfig.Ssh.Args
		}
	}

	return &config, nil
}

type BackgroundCommandLogger struct {
	Prefix string
}

func (b *BackgroundCommandLogger) Write(p []byte) (int, error) {
	lines := strings.Split(bytes.NewBuffer(p).String(), "\n")
	for _, l := range lines {
		log.Debug("%s: %s", b.Prefix, l)
	}
	return len(p), nil
}

func LaunchBackgroundCommand(command string, done <-chan struct{}, debug bool) {
	if command == "" {
		return
	}

	args := strings.Split(command, " ")
	cmd := exec.Command(args[0], args[1:]...)

	if debug {
		stdout_log := &BackgroundCommandLogger{"bg_command.stdout"}
		stderr_log := &BackgroundCommandLogger{"bg_command.stderr"}
		cmd.Stdout = stdout_log
		cmd.Stderr = stderr_log
	}

	if err := cmd.Start(); err != nil {
		log.Error("Error launching background command: %s", err)
		return
	}

	defer func() {
		// Send a SIGKILL when leaving.
		// XXX Maybe could we send a SIGTERM instead and then a
		// SIGKILL after a timeout?
		cmd.Process.Kill()
		cmd.Wait()
	}()

	select {
	case <-done:
		return
	}
}

func main() {
	config_file := defaultConfig
	if len(os.Args) > 1 {
		config_file = os.Args[1]
		if config_file == "-h" || config_file == "--help" {
			fmt.Fprintf(os.Stderr, "usage: sshproxy [config]\n")
			os.Exit(0)
		}
	}

	current_user, err := user.Current()
	if err != nil {
		log.Fatalf("Cannot find current user: %s", err)
	}
	username := current_user.Username

	ssh_connection := os.Getenv("SSH_CONNECTION")
	if ssh_connection == "" {
		log.Fatal("No SSH_CONNECTION environment variable")
	}

	src := regexp.MustCompile(`([0-9\.]+) ([0-9]+) [0-9\.]+ [0-9]+`).ReplaceAllString(ssh_connection, "$1:$2")
	if src == ssh_connection {
		log.Fatalf("parsing SSH_CONNECTION: bad value '%s'", ssh_connection)
	}

	config, err := LoadConfig(config_file, username)
	if err != nil {
		log.Fatalf("Reading configuration '%s': %s", config_file, err)
	}

	MustSetupLogging(config.Log, username, src, config.Debug)

	log.Debug("debug = %v", config.Debug)
	log.Debug("log = %s", config.Log)
	log.Debug("bg_command = %s", config.Bg_Command)
	log.Debug("ssh.exe = %s", config.Ssh.Exe)
	log.Debug("ssh.destination = %s", config.Ssh.Destination)
	log.Debug("ssh.args = %v", config.Ssh.Args)

	log.Notice("connected")
	defer log.Notice("disconnected")

	// waitgroup and channel to stop our background command when exiting.
	var wg sync.WaitGroup
	done := make(chan struct{})
	defer func() {
		close(done)
		wg.Wait()
	}()

	// launch background command
	go func() {
		wg.Add(1)
		defer wg.Done()
		LaunchBackgroundCommand(config.Bg_Command, done, config.Debug)
	}()

	original_cmd := os.Getenv("SSH_ORIGINAL_COMMAND")
	log.Debug("original_cmd = %s", original_cmd)

	// We assume the `sftp-server` binary is in the same directory on the
	// gateway as on the target.
	ssh_args := append(config.Ssh.Args, config.Ssh.Destination, original_cmd)
	cmd := exec.Command(config.Ssh.Exe, ssh_args...)
	log.Debug("command = %s %q", cmd.Path, cmd.Args)

	// We can modify those if we want to record session.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("error executing command: %s", err)
	}
}
