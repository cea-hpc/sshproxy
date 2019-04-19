// +build docker

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var etcdEnv = []string{
	"ETCDCTL_API=3",
	"ETCDCTL_CACERT=/etc/etcd/ca.pem",
	"ETCDCTL_ENDPOINTS=https://etcd:2379",
	"ETCDCTL_CERT=/etc/etcd/sshproxy.pem",
	"ETCDCTL_KEY=/etc/etcd/sshproxy-key.pem",
}

var (
	SSHPROXYCTL    = "/usr/bin/sshproxyctl"
	SSHPROXYCONFIG = "/etc/sshproxy/sshproxy.yaml"
)

func addLineSSHProxyConf(line string) {
	ctx := context.Background()
	_, _, _, err := runCommand(ctx, "ssh", []string{"root@gateway", "--", fmt.Sprintf("echo \"%s\" >> %s", line, SSHPROXYCONFIG)}, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func removeLineSSHProxyConf(line string) {
	ctx := context.Background()
	_, _, _, err := runCommand(ctx, "ssh", []string{"root@gateway", "--", fmt.Sprintf("sed -i 's/^%s$//' %s", line, SSHPROXYCONFIG)}, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func runCommand(ctx context.Context, name string, args []string, env []string, processChan chan *os.Process) (int, []byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	if processChan != nil {
		processChan <- cmd.Process
	}

	stdout, _ := ioutil.ReadAll(stdoutPipe)
	stderr, _ := ioutil.ReadAll(stderrPipe)

	err = cmd.Wait()
	rc := cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()

	return rc, stdout, stderr, err
}

func prepareCommand(port int, command string) ([]string, string) {
	args := []string{"-p", strconv.Itoa(port), "gateway"}
	if command != "" {
		args = append(args, "--", command)
	}
	cmd := fmt.Sprintf("ssh %s", strings.Join(args, " "))
	return args, cmd
}

func setupEtcd() {
	ctx := context.Background()
	filename := filepath.Join(os.Getenv("HOME"), ".etcd_setup")
	_, err := os.Stat(filename)
	if err == nil {
		return
	}
	if !os.IsNotExist(err) {
		log.Fatalf("error: stat(%s): %v", filename, err)
	}

	commands := [][]string{
		[]string{"role", "add", "sshproxy"},
		[]string{"role", "grant-permission", "sshproxy", "--prefix=true", "readwrite", "/sshproxy/"},
		[]string{"user", "add", "sshproxy:sshproxy"},
		[]string{"user", "grant-role", "sshproxy", "sshproxy"},
		[]string{"user", "add", "root:root"},
		[]string{"auth", "enable"},
	}

	for _, cmd := range commands {
		runCommand(ctx, "etcdctl", cmd, etcdEnv, nil)
	}

	fd, err := os.Create(filename)
	if err != nil {
		log.Fatalf("error: cannot write %s: %v", filename, err)
	}
	fd.Close()
}

type aggConnection struct {
	User string
	Host string
	Port string
	Dest string
	N    int
	Last time.Time
}

func getEtcdConnections() ([]aggConnection, string) {
	ctx := context.Background()
	_, stdout, _, err := runCommand(ctx, "ssh", []string{"gateway", "--", fmt.Sprintf("%s show -json connections", SSHPROXYCTL)}, nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	jsonStr := strings.TrimSpace(string(stdout))
	var connections []aggConnection
	if err := json.Unmarshal(stdout, &connections); err != nil {
		log.Fatal(err)
	}

	return connections, jsonStr
}

type host struct {
	Hostname string
	Port     string
	State    string
	Ts       time.Time
}

func getEtcdHosts() ([]host, string) {
	ctx := context.Background()
	_, stdout, _, err := runCommand(ctx, "ssh", []string{"gateway", "--", fmt.Sprintf("%s show -json hosts", SSHPROXYCTL)}, nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	jsonStr := strings.TrimSpace(string(stdout))
	var hosts []host
	if err := json.Unmarshal(stdout, &hosts); err != nil {
		log.Fatal(err)
	}

	return hosts, jsonStr
}

func disableHost(host string) {
	ctx := context.Background()
	_, _, _, err := runCommand(ctx, "ssh", []string{"gateway", "--", fmt.Sprintf("%s disable %s", SSHPROXYCTL, host)}, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func enableHost(host string) {
	ctx := context.Background()
	_, _, _, err := runCommand(ctx, "ssh", []string{"gateway", "--", fmt.Sprintf("%s enable %s", SSHPROXYCTL, host)}, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
}

var simpleConnectTests = []struct {
	port int
	want string
}{
	{2023, "server1"},
	{2024, "server2"},
	{2025, "server3"},
}

func TestSimpleConnect(t *testing.T) {
	for _, tt := range simpleConnectTests {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args, cmd := prepareCommand(tt.port, "hostname")
		_, stdout, stderr, err := runCommand(ctx, "ssh", args, nil, nil)
		stdoutStr := strings.TrimSpace(string(stdout))
		if err != nil {
			t.Errorf("%s unexpected error: %v | stderr = %s", cmd, err, string(stderr))
		} else if stdoutStr != tt.want {
			t.Errorf("%s hostname = %s, want %s", cmd, stdoutStr, tt.want)
		}
	}
}

func TestReturnCode(t *testing.T) {
	for _, exitCode := range []int{0, 3} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args, cmd := prepareCommand(2023, fmt.Sprintf("exit %d", exitCode))
		rc, _, _, _ := runCommand(ctx, "ssh", args, nil, nil)
		if rc != exitCode {
			t.Errorf("%s rc = %d, want %d", cmd, rc, exitCode)
		}
	}
}

func TestMainSSHDied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	args, _ := prepareCommand(2023, "sleep 60")
	ch := make(chan *os.Process, 1)
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process := <-ch
	process.Kill()
	rc, _, _, _ := runCommand(ctx, "ssh", []string{"gateway", "--", "pgrep sshproxy"}, nil, nil)
	if rc != 1 {
		t.Error("found running sshproxy on gateway")
	}
}

func TestEtcdConnections(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(1 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _ := prepareCommand(2023, "sleep 20")
	ch := make(chan *os.Process)
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process1 := <-ch

	time.Sleep(1 * time.Second)
	connections, jsonStr := getEtcdConnections()
	if len(connections) != 1 {
		t.Errorf("%s found %d connections, want 1", jsonStr, len(connections))
		return
	}

	c := connections[0]
	if c.User != "centos" || c.Port != "2023" || c.Dest != "server1:22" || c.N != 1 {
		t.Errorf("%s, want User=centos, Port=2023, Dest=server1:22, N=1", jsonStr)
	}

	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process2 := <-ch

	time.Sleep(1 * time.Second)
	connections, jsonStr = getEtcdConnections()
	if len(connections) != 1 {
		t.Errorf("%s found %d connections, want 1", jsonStr, len(connections))
		return
	}

	if connections[0].N != 2 {
		t.Errorf("%s found %d connections, want 2", jsonStr, connections[0].N)
	}

	process1.Kill()
	process2.Kill()
	time.Sleep(3 * time.Second)
	connections, jsonStr = getEtcdConnections()
	if len(connections) != 0 {
		t.Errorf("%s found %d connections, want 0", jsonStr, len(connections))
		return
	}
}

func checkHostCheck(t *testing.T, host string, check time.Time) time.Time {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args, _ := prepareCommand(2023, "hostname")
	want := time.Now()
	runCommand(ctx, "ssh", args, nil, nil)

	hosts, jsonStr := getEtcdHosts()
	found := false
	var lastCheck time.Time
	for _, h := range hosts {
		if h.Hostname == host {
			found = true
			lastCheck = h.Ts
			if check.IsZero() {
				if lastCheck.Sub(want) > 1*time.Second {
					t.Errorf("%s %s check at %s, want near %s", jsonStr, host, lastCheck, want)
				}
			} else {
				if h.Ts != check {
					t.Errorf("%s %s check at %s, want %s", jsonStr, host, lastCheck, check)
				}
			}
			break
		}
	}

	if !found {
		t.Errorf("%s cannot found entry for %s", jsonStr, host)
	}

	return lastCheck
}

func TestEtcdHosts(t *testing.T) {
	timeZero := time.Time{}

	lastCheck := checkHostCheck(t, "server1", timeZero)

	line := "check_interval: 5s"
	addLineSSHProxyConf(line)
	defer removeLineSSHProxyConf(line)
	checkHostCheck(t, "server1", lastCheck)

	time.Sleep(5 * time.Second)
	checkHostCheck(t, "server1", timeZero)
}

func checkHostState(t *testing.T, host, state string) {
	hosts, jsonStr := getEtcdHosts()
	found := false
	for _, h := range hosts {
		if h.Hostname == host {
			found = true
			if h.State != state {
				t.Errorf("%s %s state = %s, want %s", jsonStr, host, h.State, state)
			}
			break
		}
	}
	if !found {
		t.Errorf("%s cannot found entry for %s", jsonStr, host)
	}
}

func TestEnableDisableHost(t *testing.T) {
	args, cmdStr := prepareCommand(2022, "hostname")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, stdout, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest := strings.TrimSpace(string(stdout))
	if dest != "server1" {
		t.Errorf("%s got %s, expected server1", cmdStr, dest)
	}

	disableHost("server1")
	checkHostState(t, "server1", "disabled")

	_, stdout, _, err = runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest = strings.TrimSpace(string(stdout))
	if dest != "server2" {
		t.Errorf("%s got %s, expected server2", cmdStr, dest)
	}

	enableHost("server1")
	checkHostState(t, "server1", "up")

	// test stickyness
	_, stdout, _, err = runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest = strings.TrimSpace(string(stdout))
	if dest != "server2" {
		t.Errorf("%s got %s, expected server2", cmdStr, dest)
	}

	// entry should be removed after 3 seconds
	time.Sleep(3 * time.Second)
	_, stdout, _, err = runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest = strings.TrimSpace(string(stdout))
	if dest != "server1" {
		t.Errorf("%s got %s, expected server1", cmdStr, dest)
	}
}

// XXX sftp-server / internal-sftp tests

func waitForServers(hostports []string, timeout time.Duration) {
	var wg sync.WaitGroup
	results := make([]bool, len(hostports))
	timeoutTimer := time.After(timeout)
	for i, hostport := range hostports {
		wg.Add(1)
		go func(n int, dest string) {
			defer wg.Done()
			for {
				select {
				case <-timeoutTimer:
					results[n] = false
					return
				case <-time.After(1 * time.Second):
					c, err := net.DialTimeout("tcp", dest, 1*time.Second)
					if err == nil {
						c.Close()
						results[n] = true
						return
					}
				}
			}
		}(i, hostport)
	}
	wg.Wait()

	for _, b := range results {
		if !b {
			log.Fatalf("cannot connect to %s after %s", hostports, timeout)
		}
	}
}

// TestMain is the main function for testing.
func TestMain(m *testing.M) {
	waitForServers([]string{"etcd:2379", "gateway:22", "gateway:2022", "gateway:2023", "gateway:2024", "gateway:2025"}, 1*time.Minute)
	setupEtcd()
	os.Exit(m.Run())
}
