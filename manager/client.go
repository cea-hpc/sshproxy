package manager

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// A Client represents a connection to a sshproxy-managerd.
type Client struct {
	Manager  string        // Address of the sshproxy-managerd
	Username string        // Username to connect/disconnect
	Sshd     string        // Address of reached sshd
	Timeout  time.Duration // Connection timeout
}

// NewClient initializes a new Client.
func NewClient(manager, username, sshd string, timeout time.Duration) *Client {
	return &Client{manager, username, sshd, timeout}
}

// sendCommand sends command to a manager (adding '\r\n').
// It returns the response as a string or an error.
func (c *Client) sendCommand(command string, expect_response bool) (string, error) {
	var conn net.Conn
	var err error
	if c.Timeout == 0 {
		conn, err = net.Dial("tcp", c.Manager)
	} else {
		conn, err = net.DialTimeout("tcp", c.Manager, c.Timeout)
	}
	if err != nil {
		return "", fmt.Errorf("connecting to %s: %s", c.Manager, err)
	}
	defer conn.Close()

	// set a deadline for sending command and receiving response
	conn.SetDeadline(time.Now().Add(2 * c.Timeout))

	data := fmt.Sprintf("%s\r\n", command)
	n, err := io.WriteString(conn, data)
	if err != nil {
		return "", fmt.Errorf("writing to %s: %s", c.Manager, err)
	}
	if n != len(data) {
		return "", fmt.Errorf("partial write to %s", c.Manager)
	}

	if expect_response {
		buf := new(bytes.Buffer)
		if _, err := io.Copy(buf, conn); err != nil {
			return "", fmt.Errorf("reading from %s: %s", c.Manager, err)
		}
		return buf.String(), nil
	}

	return "", nil
}

// Connect sends a connection request to the manager.
// It returns an IP address with a port or an error.
func (c *Client) Connect() (string, string, error) {
	line, err := c.sendCommand(fmt.Sprintf("connect %s %s", c.Username, c.Sshd), true)
	if err != nil {
		return "", "", err
	}
	hostport := strings.TrimSpace(line)
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", "", fmt.Errorf("invalid response from %s [=%s]: %s", c.Manager, hostport, err)
	}
	return host, port, nil
}

// Disconnect sends a disconnection request to the manager.
// It returns an error if any.
func (c *Client) Disconnect() error {
	_, err := c.sendCommand(fmt.Sprintf("disconnect %s %s", c.Username, c.Sshd), false)
	return err
}

// Disconnect sends a failure request to the manager for the specified destination.
// It returns an error if any.
func (c *Client) Failure(dest string) error {
	_, err := c.sendCommand(fmt.Sprintf("failure %s", dest), false)
	return err
}
