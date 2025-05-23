//go:build docker
// +build docker

package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

var gateways = []string{
	"gateway1",
	"gateway2",
}

var (
	SSHPROXYCTL    = "/usr/bin/sshproxyctl"
	SSHPROXYCONFIG = "/etc/sshproxy/sshproxy.yaml"
)

func addLineSSHProxyConf(line string) {
	ctx := context.Background()
	for _, gateway := range gateways {
		_, _, _, err := runCommand(ctx, "ssh", []string{fmt.Sprintf("root@%s", gateway), "--", fmt.Sprintf("echo \"%s\" >> %s", line, SSHPROXYCONFIG)}, nil, nil)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func removeLineSSHProxyConf(line string) {
	ctx := context.Background()
	line = strings.ReplaceAll(line, "/", "\\/")
	for _, gateway := range gateways {
		_, _, _, err := runCommand(ctx, "ssh", []string{fmt.Sprintf("root@%s", gateway), "--", fmt.Sprintf("sed -i 's/^%s$//' %s", line, SSHPROXYCONFIG)}, nil, nil)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func updateLineSSHProxyConf(key string, value string) {
	ctx := context.Background()
	value = strings.ReplaceAll(value, "/", "\\/")
	for _, gateway := range gateways {
		_, _, _, err := runCommand(ctx, "ssh", []string{fmt.Sprintf("root@%s", gateway), "--", fmt.Sprintf("sed -i '/%s:/s/: .*$/: %s/' %s", key, value, SSHPROXYCONFIG)}, nil, nil)
		if err != nil {
			log.Fatal(err)
		}
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

	stdout, _ := io.ReadAll(stdoutPipe)
	stderr := []byte{}
	if ctx.Err() != context.DeadlineExceeded {
		stderr, _ = io.ReadAll(stderrPipe)
	}

	err = cmd.Wait()
	rc := cmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()

	return rc, stdout, stderr, err
}

func prepareCommand(gateway string, port int, command string) ([]string, string) {
	args := []string{"-p", strconv.Itoa(port), gateway}
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

	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("error: cannot write %s: %v", filename, err)
	}
	f.Close()
}

type aggConnection struct {
	User    string
	Service string
	Dest    string
	N       int
	Last    time.Time
}

func getEtcdConnections() ([]aggConnection, string) {
	_, stdout, _, err := etcdCommand("show -json connections")
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

type aggUser struct {
	User    string
	Service string
	Groups  string
	N       int
	BwIn    int
	BwOut   int
	Dest    string
	TTL     int
}

func getEtcdAllUsers() ([]aggUser, string) {
	ctx := context.Background()
	_, stdout, _, err := runCommand(ctx, "ssh", []string{"gateway1", "--", fmt.Sprintf("%s show -json users -all", SSHPROXYCTL)}, nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	jsonStr := strings.TrimSpace(string(stdout))
	var users []aggUser
	if err := json.Unmarshal(stdout, &users); err != nil {
		log.Fatal(err)
	}

	return users, jsonStr
}

type host struct {
	Hostname string
	Port     string
	State    string
	Ts       time.Time
}

func getEtcdHosts() ([]host, string) {
	_, stdout, _, err := etcdCommand("show -json hosts")
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
	_, _, _, err := etcdCommand(fmt.Sprintf("disable %s", host))
	if err != nil {
		log.Fatal(err)
	}
}

func enableHost(host string) {
	_, _, _, err := etcdCommand(fmt.Sprintf("enable %s", host))
	if err != nil {
		log.Fatal(err)
	}
}

func forgetHost(host string) error {
	_, _, _, err := etcdCommand(fmt.Sprintf("forget host %s", host))
	return err
}

func forgetPersist() error {
	_, _, _, err := etcdCommand("forget persist -port 22")
	return err
}

type user struct {
	User    string
	Group   string
	Service string
	N       int
}

func getEtcdUsers(mode string, allFlag bool) (map[string]user, string) {
	all := ""
	if allFlag {
		all = " -all"
	}
	_, stdout, _, err := etcdCommand(fmt.Sprintf("show -json %s%s", mode, all))
	if err != nil {
		log.Fatal(err)
	}

	jsonStr := strings.TrimSpace(string(stdout))
	var users []user
	if err := json.Unmarshal(stdout, &users); err != nil {
		log.Fatal(err)
	}
	usersMap := map[string]user{}
	for _, user := range users {
		key := ""
		if user.User != "" {
			key = user.User
		} else {
			key = user.Group
		}
		if allFlag {
			key += "@" + user.Service
		}
		usersMap[key] = user
	}

	return usersMap, jsonStr
}

func setEtcdErrorBanner(banner string) {
	_, _, _, err := etcdCommand(fmt.Sprintf("error_banner '%s'", banner))
	if err != nil {
		log.Fatal(err)
	}
}

func forgetEtcdErrorBanner() {
	_, _, _, err := etcdCommand("forget error_banner")
	if err != nil {
		log.Fatal(err)
	}
}

func etcdCommand(command string) (int, []byte, []byte, error) {
	ctx := context.Background()
	return runCommand(ctx, "ssh", []string{"gateway1", "--", fmt.Sprintf("%s %s", SSHPROXYCTL, command)}, nil, nil)
}

var simpleConnectTests = []struct {
	user string
	port int
	want string
}{
	{"", 2023, "server1"},
	{"", 2024, "server2"},
	{"", 2025, "server3"},
	{"user1@", 2023, "server2"},
	{"user1@", 2024, "server2"},
	{"user2@", 2023, "server2"},
	{"user2@", 2024, "server1"},
}

func TestSimpleConnect(t *testing.T) {
	for _, tt := range simpleConnectTests {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args, cmd := prepareCommand(tt.user+"gateway1", tt.port, "hostname")
		_, stdout, stderr, err := runCommand(ctx, "ssh", args, nil, nil)
		stdoutStr := strings.TrimSpace(string(stdout))
		if err != nil {
			t.Errorf("%s unexpected error: %v | stderr = %s", cmd, err, string(stderr))
		} else if stdoutStr != tt.want {
			t.Errorf("%s hostname = %s, want %s", cmd, stdoutStr, tt.want)
		}
	}
}

func TestBlockingCommand(t *testing.T) {
	addLineSSHProxyConf("blocking_command: /bin/true")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args, cmd := prepareCommand("gateway1", 2023, "hostname")
	_, stdout, stderr, err := runCommand(ctx, "ssh", args, nil, nil)
	stdoutStr := strings.TrimSpace(string(stdout))
	if err != nil {
		t.Errorf("%s unexpected error: %v | stderr = %s", cmd, err, string(stderr))
	} else if stdoutStr != "server1" {
		t.Errorf("%s hostname = %s, want server1", cmd, stdoutStr)
	}
	updateLineSSHProxyConf("blocking_command", "/bin/false")
	defer removeLineSSHProxyConf("blocking_command: /bin/false")
	args, _ = prepareCommand("gateway1", 2023, "hostname")
	rc, _, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err == nil {
		t.Errorf("got no error, expected error due to blocking_command")
	} else if rc != 1 {
		t.Errorf("blocking_command rc = %d, want 1", rc)
	}
}

func TestNodesets(t *testing.T) {
	disableHost("-host server[1000-1002]")
	checkHostState(t, "server1000:22", "disabled", true)
	checkHostState(t, "server1001:22", "disabled", true)
	checkHostState(t, "server1002:22", "disabled", true)
	enableHost("-host server[1000-1002]")
	checkHostState(t, "server1000:22", "up", true)
	checkHostState(t, "server1001:22", "up", true)
	checkHostState(t, "server1002:22", "up", true)
	err := forgetHost("-host server[1001]")
	if err != nil {
		t.Errorf("got %s, expected no error", err)
	}
	checkHostState(t, "server1000:22", "up", true)
	checkHostState(t, "server1001:22", "", false)
	checkHostState(t, "server1002:22", "up", true)
	err = forgetHost("-host server[1000-1002]")
	if err != nil {
		t.Errorf("got %s, expected no error", err)
	}
	checkHostState(t, "server1000:22", "", false)
	checkHostState(t, "server1001:22", "", false)
	checkHostState(t, "server1002:22", "", false)
	err = forgetHost("-host server[12345]")
	if err != nil {
		t.Errorf("got %s, expected no error", err)
	}
	checkHostState(t, "server12345:22", "", false)
	if forgetHost("-host server[notAnumber]") == nil {
		t.Errorf("got no error, expected error due to notAnumber not being a number")
	}
}

var environmentTests = []struct {
	user string
	port int
	want string
}{
	{"", 2023, "globalEnv_fedora"},
	{"", 2024, "serviceEnv_fedora"},
	{"user2@", 2023, "globalUserEnv_user2"},
	{"user2@", 2024, "serviceUserEnv_user2"},
}

func TestEnvironment(t *testing.T) {
	for _, tt := range environmentTests {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args, cmd := prepareCommand(tt.user+"gateway1", tt.port, "echo $XMODIFIERS")
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
		args, cmd := prepareCommand("gateway1", 2023, fmt.Sprintf("exit %d", exitCode))
		rc, _, _, _ := runCommand(ctx, "ssh", args, nil, nil)
		if rc != exitCode {
			t.Errorf("%s rc = %d, want %d", cmd, rc, exitCode)
		}
	}
}

func TestMainSSHDied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2023, "sleep 60")
	ch := make(chan *os.Process, 1)
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process := <-ch
	process.Kill()
	rc, _, _, _ := runCommand(ctx, "ssh", []string{"gateway1", "--", "pgrep sshproxy"}, nil, nil)
	if rc != 1 {
		t.Error("found running sshproxy on gateway1")
	}
}

func TestEtcdConnections(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(4 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2023, "sleep 20")
	ch := make(chan *os.Process)
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process1 := <-ch

	time.Sleep(time.Second)
	connections, jsonStr := getEtcdConnections()
	if len(connections) != 1 {
		t.Errorf("%s found %d connections, want 1", jsonStr, len(connections))
		return
	}

	c := connections[0]
	if c.User != "fedora" || c.Service != "service2" || c.Dest != "server1:22" || c.N != 1 {
		t.Errorf("%s, want User=fedora, Service=service2, Dest=server1:22, N=1", jsonStr)
	}

	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process2 := <-ch

	time.Sleep(time.Second)
	connections, jsonStr = getEtcdConnections()
	if len(connections) != 1 {
		t.Errorf("%s found %d different connections, want 1", jsonStr, len(connections))
		return
	}

	if connections[0].N != 2 {
		t.Errorf("%s found %d aggregated connections, want 2", jsonStr, connections[0].N)
	}

	process1.Kill()
	process2.Kill()
	time.Sleep(4 * time.Second)
	connections, jsonStr = getEtcdConnections()
	if len(connections) != 0 {
		t.Errorf("%s found %d connections, want 0", jsonStr, len(connections))
		return
	}
}

func TestWithoutEtcd(t *testing.T) {
	updateLineSSHProxyConf("endpoints", "[\"https://void:2379\"]")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, cmdStr := prepareCommand("gateway1", 2023, "hostname")
	_, stdout, _, err := runCommand(ctx, "ssh", args, nil, nil)
	updateLineSSHProxyConf("endpoints", "[\"https://etcd:2379\"]")
	if err != nil {
		log.Fatal(err)
	}
	dest := strings.TrimSpace(string(stdout))
	if dest != "server1" {
		t.Errorf("%s got %s, expected server1", cmdStr, dest)
	}

	updateLineSSHProxyConf("endpoints", "[\"https://void:2379\"]")
	updateLineSSHProxyConf("mandatory", "true")
	_, _, _, err = runCommand(ctx, "ssh", args, nil, nil)
	updateLineSSHProxyConf("endpoints", "[\"https://etcd:2379\"]")
	updateLineSSHProxyConf("mandatory", "false")
	if err == nil {
		t.Error("the connection should have been rejected")
	}
}

func TestMaxConnectionsPerUser(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(4 * time.Second)

	updateLineSSHProxyConf("max_connections_per_user", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2023, "sleep 20")
	ch := make(chan *os.Process)
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process1 := <-ch

	time.Sleep(time.Second)

	args, _ = prepareCommand("gateway1", 2023, "hostname")
	_, _, _, err := runCommand(ctx, "ssh", args, nil, nil)
	process1.Kill()
	updateLineSSHProxyConf("max_connections_per_user", "0")
	if err == nil {
		t.Error("the second connection should have been rejected")
	}
}

func TestStickyConnections(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(4 * time.Second)

	disableHost("-host server1")
	checkHostState(t, "server1:22", "disabled", true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2022, "sleep 20")
	ch := make(chan *os.Process)
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process1 := <-ch

	time.Sleep(time.Second)
	enableHost("-host server1")
	checkHostState(t, "server1:22", "up", true)

	args, cmdStr := prepareCommand("gateway2", 2022, "hostname")
	_, stdout, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	process1.Kill()
	dest := strings.TrimSpace(string(stdout))
	if dest != "server2" {
		t.Errorf("%s got %s, expected server2", cmdStr, dest)
	}
}

func TestNotLongStickyConnections(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(4 * time.Second)

	disableHost("-host server1")
	checkHostState(t, "server1:22", "disabled", true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2022, "hostname")
	_, _, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	time.Sleep(2 * time.Second)
	enableHost("-host server1")
	checkHostState(t, "server1:22", "up", true)

	args, cmdStr := prepareCommand("gateway2", 2022, "hostname")
	_, stdout, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest := strings.TrimSpace(string(stdout))
	if dest != "server1" {
		t.Errorf("%s got %s, expected server1", cmdStr, dest)
	}
}

func TestLongStickyConnections(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(4 * time.Second)

	updateLineSSHProxyConf("etcd_keyttl", "10")
	disableHost("-host server1")
	checkHostState(t, "server1:22", "disabled", true)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2022, "hostname")
	_, _, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}

	time.Sleep(2 * time.Second)
	enableHost("-host server1")
	checkHostState(t, "server1:22", "up", true)

	args, cmdStr := prepareCommand("gateway2", 2022, "hostname")
	_, stdout, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	updateLineSSHProxyConf("etcd_keyttl", "0")
	dest := strings.TrimSpace(string(stdout))
	if dest != "server2" {
		t.Errorf("%s got %s, expected server2", cmdStr, dest)
	}
}

func TestForgetPersist(t *testing.T) {
	updateLineSSHProxyConf("etcd_keyttl", "3600")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2022, "hostname")
	_, stdout, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest := strings.TrimSpace(string(stdout)) + ":22"

	users, _ := getEtcdAllUsers()
	if users[0].Dest != dest {
		t.Errorf("'Persist to' is %s, want %s", users[0].Dest, dest)
	}
	err = forgetPersist()
	if err != nil {
		log.Fatal(err)
	}
	updateLineSSHProxyConf("etcd_keyttl", "0")
	users, _ = getEtcdAllUsers()
	if users[0].Dest != "" {
		t.Errorf("'Persist to' is %s, want empty string", users[0].Dest)
	}
}

func TestBalancedConnections(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(4 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2022, "sleep 20")
	ch := make(chan *os.Process)
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process1 := <-ch

	time.Sleep(time.Second)
	updateLineSSHProxyConf("route_select", "connections")
	updateLineSSHProxyConf("mode", "balanced")

	args, cmdStr := prepareCommand("gateway2", 2022, "hostname")
	_, stdout, _, err := runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	process1.Kill()
	updateLineSSHProxyConf("route_select", "ordered")
	updateLineSSHProxyConf("mode", "sticky")
	dest := strings.TrimSpace(string(stdout))
	if dest != "server2" {
		t.Errorf("%s got %s, expected server2", cmdStr, dest)
	}
}

func checkHostCheck(t *testing.T, host string, check time.Time) time.Time {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2023, "hostname")
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

	lastCheck := checkHostCheck(t, "server1:22", timeZero)

	line := "check_interval: 5s"
	addLineSSHProxyConf(line)
	defer removeLineSSHProxyConf(line)
	checkHostCheck(t, "server1:22", lastCheck)

	time.Sleep(5 * time.Second)
	checkHostCheck(t, "server1:22", timeZero)
}

func checkHostState(t *testing.T, host, state string, wantFound bool) {
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
	if wantFound && !found {
		t.Errorf("%s cannot find entry for %s", jsonStr, host)
	} else if !wantFound && found {
		t.Errorf("%s should not contain entry %s", jsonStr, host)
	}
}

func TestEnableDisableHost(t *testing.T) {
	args, cmdStr := prepareCommand("gateway1", 2022, "hostname")
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

	disableHost("-host server[1,100]")
	checkHostState(t, "server1:22", "disabled", true)

	_, stdout, _, err = runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest = strings.TrimSpace(string(stdout))
	if dest != "server2" {
		t.Errorf("%s got %s, expected server2", cmdStr, dest)
	}

	enableHost("-host server1")
	checkHostState(t, "server1:22", "up", true)

	// test stickiness
	_, stdout, _, err = runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest = strings.TrimSpace(string(stdout))
	if dest != "server2" {
		t.Errorf("%s got %s, expected server2", cmdStr, dest)
	}

	// entry should be removed after 4 seconds
	time.Sleep(4 * time.Second)
	_, stdout, _, err = runCommand(ctx, "ssh", args, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	dest = strings.TrimSpace(string(stdout))
	if dest != "server1" {
		t.Errorf("%s got %s, expected server1", cmdStr, dest)
	}
}

func TestEtcdUsers(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(4 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch := make(chan *os.Process)
	args, _ := prepareCommand("gateway1", 2023, "sleep 20")
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process1 := <-ch
	defer process1.Kill()

	args, _ = prepareCommand("gateway1", 2024, "sleep 20")
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process2 := <-ch
	defer process2.Kill()

	time.Sleep(time.Second)
	users, jsonStr := getEtcdUsers("users", false)
	if len(users) != 1 {
		t.Errorf("%s found %d aggregated users, want 1", jsonStr, len(users))
		return
	} else if users["fedora"].N != 2 {
		t.Errorf("%s found %d aggregated user connections, want 2", jsonStr, users["fedora"].N)
		return
	}
	users, jsonStr = getEtcdUsers("users", true)
	if len(users) != 2 {
		t.Errorf("%s found %d users, want 2", jsonStr, len(users))
		return
	} else if users["fedora@service2"].N != 1 {
		t.Errorf("%s found %d user connections, want 1", jsonStr, users["fedora@service2"].N)
		return
	} else if users["fedora@service3"].N != 1 {
		t.Errorf("%s found %d user connections, want 1", jsonStr, users["fedora@service3"].N)
	}
}

func TestEtcdGroups(t *testing.T) {
	// remove old connections stored in etcd
	time.Sleep(4 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch := make(chan *os.Process)
	args, _ := prepareCommand("gateway1", 2023, "sleep 20")
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process1 := <-ch
	defer process1.Kill()

	args, _ = prepareCommand("gateway1", 2024, "sleep 20")
	go func() {
		runCommand(ctx, "ssh", args, nil, ch)
	}()
	process2 := <-ch
	defer process2.Kill()

	time.Sleep(time.Second)
	groups, jsonStr := getEtcdUsers("groups", false)
	if len(groups) != 1 {
		t.Errorf("%s found %d aggregated groups, want 1", jsonStr, len(groups))
		return
	} else if groups["fedora"].N != 2 {
		t.Errorf("%s found %d aggregated group connections, want 2", jsonStr, groups["fedora"].N)
		return
	}
	groups, jsonStr = getEtcdUsers("groups", true)
	if len(groups) != 2 {
		t.Errorf("%s found %d groups, want 2", jsonStr, len(groups))
		return
	} else if groups["fedora@service2"].N != 1 {
		t.Errorf("%s found %d group connections, want 1", jsonStr, groups["fedora@service2"].N)
		return
	} else if groups["fedora@service3"].N != 1 {
		t.Errorf("%s found %d group connections, want 1", jsonStr, groups["fedora@service3"].N)
	}
}

func prepareSFTPBatchCommands(filename, downloadFilename string) {
	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("error: cannot write %s: %v", filename, err)
	}
	defer f.Close()
	f.WriteString(fmt.Sprintf("get /etc/passwd %s\n!sleep 5\n", downloadFilename))
}

func hash(filename string) []byte {
	f, err := os.Open(filename)
	if err != nil {
		log.Fatalf("error: cannot open %s: %v", filename, err)
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Fatal(err)
	}

	return h.Sum(nil)
}

var sftpTests = []struct {
	server string
	dest   string
	port   int
}{
	{"sftp-server", "server1", 2023},
	{"internal-sftp", "server2", 2024},
}

func TestSFTP(t *testing.T) {
	refSum := hash("/etc/passwd")

	for i, tt := range sftpTests {
		batchFile := fmt.Sprintf("/tmp/sftp.batch.%d", i)
		downloadFile := fmt.Sprintf("/tmp/passwd.%d", i)
		prepareSFTPBatchCommands(batchFile, downloadFile)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ch := make(chan *os.Process, 1)
		go func() {
			runCommand(ctx, "sftp", []string{"-P", strconv.Itoa(tt.port), "-b", batchFile, "gateway1"}, nil, ch)
		}()
		process := <-ch

		time.Sleep(time.Second)

		rc, _, _, _ := runCommand(ctx, "ssh", []string{tt.dest, "--", fmt.Sprintf("pgrep -f %s", tt.server)}, nil, nil)
		if rc != 0 {
			t.Errorf("cannot find '%s' running on %s", tt.server, tt.dest)
		}

		process.Kill()

		sum := hash(downloadFile)
		if !reflect.DeepEqual(refSum, sum) {
			t.Errorf("MD5 are different: got %v, want %v", sum, refSum)
		}
	}
}

func TestOnlySFTP(t *testing.T) {
	refSum := hash("/etc/passwd")

	batchFile := "/tmp/sftp.batch.onlySFTP"
	downloadFile := "/tmp/passwd.onlySFTP"
	prepareSFTPBatchCommands(batchFile, downloadFile)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ch := make(chan *os.Process, 1)
	go func() {
		runCommand(ctx, "sftp", []string{"-P", strconv.Itoa(2023), "-b", batchFile, "gateway2"}, nil, ch)
	}()
	process := <-ch

	time.Sleep(time.Second)

	rc, _, _, _ := runCommand(ctx, "ssh", []string{"server1", "--", "pgrep -f sftp-server"}, nil, nil)
	if rc != 0 {
		t.Error("cannot find sftp-server running on server1")
	}

	process.Kill()

	sum := hash(downloadFile)
	if !reflect.DeepEqual(refSum, sum) {
		t.Errorf("MD5 are different: got %v, want %v", sum, refSum)
	}

	// We have tested the sftp connection, now the non-sftp connection should fail
	args, cmd := prepareCommand("gateway2", 2023, "exit 0")
	rc, _, _, _ = runCommand(ctx, "ssh", args, nil, nil)
	if rc != 1 {
		t.Errorf("%s rc = %d, want 1", cmd, rc)
	}
}

var scpTests = []struct {
	source string
	dest   string
	port   int
}{
	{"/etc/passwd", "gateway1:remoteSCP", 2022},
	{"gateway1:remoteSCP", "/tmp/finalSCP", 2022},
}

func TestSCP(t *testing.T) {
	refSum := hash("/etc/passwd")

	mode := "without dump"
	for i := 0; i < 2; i++ {
		if i != 0 {
			mode = "with dump"
			line := "dump: etcd"
			addLineSSHProxyConf(line)
			defer removeLineSSHProxyConf(line)
			line = "etcd_stats_interval: 5s"
			addLineSSHProxyConf(line)
			defer removeLineSSHProxyConf(line)
		}
		for _, tt := range scpTests {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _, _, err := runCommand(ctx, "scp", []string{"-P", strconv.Itoa(tt.port), tt.source, tt.dest}, nil, nil)
			if err != nil {
				t.Errorf("scp -P %d %s %s (%s): %s", tt.port, tt.source, tt.dest, mode, err)
			}
		}
		sum := hash("/tmp/finalSCP")
		if !reflect.DeepEqual(refSum, sum) {
			t.Errorf("MD5 are different: got %v, want %v (%s)", sum, refSum, mode)
		}
	}
}

func compareSshToErrorBanner(errorBanner string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args, _ := prepareCommand("gateway1", 2023, "hostname")
	_, stdout, _, err := runCommand(ctx, "ssh", args, nil, nil)
	stdoutStr := strings.TrimSpace(string(stdout))
	if err == nil {
		return "Expected error because all hosts are disabled, got no error"
	} else if stdoutStr != errorBanner {
		return fmt.Sprintf("got error = %s, want %s", stdoutStr, errorBanner)
	}
	return ""
}

func TestErrorBanner(t *testing.T) {
	disableHost("-all")
	defer enableHost("-all")
	defaultError := "a default error"
	line := fmt.Sprintf("error_banner: %s", defaultError)
	addLineSSHProxyConf(line)
	defer removeLineSSHProxyConf(line)

	customError := "a custom error"
	setEtcdErrorBanner(customError)
	if errStr := compareSshToErrorBanner(customError); errStr != "" {
		t.Error(errStr)
	}

	forgetEtcdErrorBanner()
	if errStr := compareSshToErrorBanner(defaultError); errStr != "" {
		t.Error(errStr)
	}
}

func waitForServers(hostports []string, timeout time.Duration) {
	results := make([]bool, len(hostports))
	ticker := time.NewTicker(time.Second)
	done := make(chan bool, 1)
	var wg sync.WaitGroup

	for i, hostport := range hostports {
		wg.Add(1)
		go func(n int, dest string) {
			defer wg.Done()
			for {
				c, err := net.DialTimeout("tcp", dest, time.Second)
				if err == nil {
					c.Close()
					results[n] = true
					return
				}

				select {
				case <-done:
					results[n] = false
					return
				case <-ticker.C:
				}
			}
		}(i, hostport)
	}

	go func() {
		<-time.After(timeout)
		close(done)
	}()

	wg.Wait()
	ticker.Stop()

	for _, b := range results {
		if !b {
			log.Fatalf("cannot connect to %s after %s", hostports, timeout)
		}
	}
}

// TestMain is the main function for testing.
func TestMain(m *testing.M) {
	waitForServers([]string{
		"etcd:2379",
		"gateway1:22",
		"gateway1:2022",
		"gateway1:2023",
		"gateway1:2024",
		"gateway1:2025",
		"gateway2:22",
		"gateway2:2022",
		"gateway2:2023",
		"gateway2:2024",
		"gateway2:2025",
	}, 2*time.Minute)
	setupEtcd()
	os.Exit(m.Run())
}
