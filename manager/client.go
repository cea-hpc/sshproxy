// Copyright 2015-2017 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package manager

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
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
func (c *Client) sendCommand(command string) (string, error) {
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

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, conn); err != nil {
		return "", fmt.Errorf("reading from %s: %s", c.Manager, err)
	}

	resp := buf.String()
	if strings.TrimSpace(resp) == "" {
		return "", fmt.Errorf("received empty response from %s", c.Manager)
	}

	fields := strings.SplitN(resp, "\r\n", 2)
	header := fields[0]

	switch {
	case header[0] == '+':
		return header[1:], nil
	case header[0] == '-':
		errmsg := header[1:]
		if strings.HasPrefix(header, "-ERR ") {
			errmsg = header[5:]
		}
		return "", fmt.Errorf("received error from %s: %s", c.Manager, errmsg)
	case header[0] == '$':
		datalen, err := strconv.Atoi(header[1:])
		if err != nil {
			return "", fmt.Errorf("invalid response from %s: %s", c.Manager, resp)
		}
		if datalen == -1 {
			return "", nil
		}
		data = fields[1][:datalen]
		if len(data) != datalen {
			return "", fmt.Errorf("missing data from %s: %s", c.Manager, resp)
		}
		return data, nil
	}

	return "", fmt.Errorf("unknown response from %s: %s", c.Manager, resp)
}

// Connect sends a connection request to the manager.
// It returns a string "host:port" or an error.
func (c *Client) Connect() (string, error) {
	hostport, err := c.sendCommand(fmt.Sprintf("connect %s %s", c.Username, c.Sshd))
	if err != nil {
		return "", err
	}
	if hostport == "" {
		return "", nil
	}
	if _, _, err = net.SplitHostPort(hostport); err != nil {
		return "", fmt.Errorf("invalid response from %s [=%s]: %s", c.Manager, hostport, err)
	}
	return hostport, nil
}

// Disconnect sends a disconnection request to the manager.
// It returns an error if any.
func (c *Client) Disconnect() error {
	_, err := c.sendCommand(fmt.Sprintf("disconnect %s %s", c.Username, c.Sshd))
	return err
}

// Failure sends a failure request to the manager for the specified destination.
// It returns an error if any.
func (c *Client) Failure(dst string) error {
	_, err := c.sendCommand(fmt.Sprintf("failure %s", dst))
	return err
}
