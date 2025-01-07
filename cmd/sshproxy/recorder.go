// Copyright 2015-2025 CEA/DAM/DIF
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
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/cea-hpc/sshproxy/pkg/record"
	"github.com/cea-hpc/sshproxy/pkg/utils"
)

// Dup duplicates a []byte slice.
func Dup(a []byte, n int) []byte {
	b := make([]byte, n)
	copy(b, a)
	return b
}

// A Splitter reads from and/or writes to a file descriptor and sends a
// record.Record struct to a channel for each read/write operation.
type Splitter struct {
	f  *os.File             // opened file
	fd int                  // integer file descriptor
	ch chan<- record.Record // channel to send record.Record structs
}

// NewSplitter returns a new Splitter struct from an already opened *os.File
// and a channel where record.Record structs will be sent.
//
// It implements the ReadWriteCloser interface.
func NewSplitter(f *os.File, ch chan record.Record) *Splitter {
	return &Splitter{f, int(f.Fd()), ch}
}

// Close implements the Closer Close method.
func (s *Splitter) Close() error {
	return s.f.Close()
}

// Read implements the Reader Read method. It sends a copy of the read slice to
// its internal channel.
func (s *Splitter) Read(p []byte) (int, error) {
	n, err := s.f.Read(p)
	pp := Dup(p, n)
	s.ch <- record.Record{
		Time: time.Now(),
		Fd:   s.fd,
		Size: n,
		Data: pp,
	}
	return n, err
}

// Write implements the Writer Write method. It sends a copy of the written
// slice to its internal channel.
func (s *Splitter) Write(p []byte) (int, error) {
	pp := Dup(p, len(p))
	s.ch <- record.Record{
		Time: time.Now(),
		Fd:   s.fd,
		Size: len(p),
		Data: pp,
	}
	return s.f.Write(p)
}

// A Recorder intercepts data read from standard input and written to standard
// output or standard error.
//
// It logs periodically basic statistics of transferred bytes and can save
// intercepted raw data in a file.
//
// The file is a succession of serialized record.Record structs. See the
// record.Record documentation for details on the format.
type Recorder struct {
	Stdin, Stdout, Stderr io.ReadWriteCloser // standard input, output and error to be used instead of the standard file descriptors
	start                 time.Time          // when the Recorder was started
	etcdStatsInterval     time.Duration      // interval at which bandwidth is updated in etcd
	logStatsInterval      time.Duration      // interval at which basic statistics of transferred bytes are logged
	bandwidth             map[int]uint64     // bytes/s for each recorded file descriptor
	totals                map[int]uint64     // total of bytes for each recorded file descriptor
	ch                    chan record.Record // channel to read record.Record structs
	conninfo              *ConnInfo          // specific SSH connection information
	command               string             // initial user command
	dumpfile              string             // path to filename where the raw records are dumped
	dumpLimitSize         uint64             // number of bytes beyond which records are no longer dumped
	dumpLimitWindow       time.Duration      // time window in which dump size is accounted
	lock                  sync.RWMutex       // mutex to avoid concurrent reads and writes in bandwidth and totals maps
	writer                *record.Writer     // *record.Writer where the raw records are dumped
}

// NewRecorder returns a new Recorder struct.
//
// If dumpfile is not empty, the intercepted raw data will be written in this
// file. Logging of basic statistics will be done every logStatsInterval seconds. Bandwidth will be updated in etcd every etcdStatsInterval seconds.
// It will stop recording when the context is cancelled.
func NewRecorder(conninfo *ConnInfo, dumpfile, command string, etcdStatsInterval time.Duration, logStatsInterval time.Duration, dumpLimitSize uint64, dumpLimitWindow time.Duration) *Recorder {
	ch := make(chan record.Record)

	return &Recorder{
		Stdin:             NewSplitter(os.Stdin, ch),
		Stdout:            NewSplitter(os.Stdout, ch),
		Stderr:            NewSplitter(os.Stderr, ch),
		etcdStatsInterval: etcdStatsInterval,
		logStatsInterval:  logStatsInterval,
		bandwidth:         map[int]uint64{0: 0, 1: 0, 2: 0},
		totals:            map[int]uint64{0: 0, 1: 0, 2: 0},
		ch:                ch,
		conninfo:          conninfo,
		command:           command,
		dumpfile:          dumpfile,
		dumpLimitSize:     dumpLimitSize,
		dumpLimitWindow:   dumpLimitWindow,
		lock:              sync.RWMutex{},
		writer:            nil,
	}
}

// updateStats writes the bandwidth to etcd
func (r *Recorder) updateStats(cli *utils.Client, etcdPath string) {
	if cli != nil && cli.IsAlive() {
		r.lock.RLock()
		stats := r.bandwidth
		r.lock.RUnlock()
		err := cli.UpdateStats(etcdPath, stats)
		if err != nil {
			log.Errorf("updating stats: %v", err)
			cli.Disable()
		}
	}
}

// log formats the internal statistics and logs them.
func (r *Recorder) log(ctx context.Context, step string) {
	fds := []string{"stdin", "stdout", "stderr"}
	t := []string{}
	r.lock.RLock()
	for fd, name := range fds {
		t = append(t, fmt.Sprintf("%s=%d", name, r.totals[fd]))
	}
	r.lock.RUnlock()
	// round to second
	elapsed := time.Duration((time.Since(r.start) / time.Second) * time.Second)
	log.Infof("bytes transferred (%s): duration=%s %s", step, elapsed, strings.Join(t, " "))
}

// dump saves a record.Record in the dumpfile.
func (r *Recorder) dump(rec record.Record) {
	if r.writer == nil {
		return
	}

	if err := r.writer.Write(&rec); err != nil {
		log.Errorf("writing: %s", err)
	}
}

// Run starts the recorder.
func (r *Recorder) Run(ctx context.Context, cli *utils.Client, etcdPath string) {
	var fd io.WriteCloser
	if r.dumpfile != "" {
		var err error
		if strings.HasPrefix(r.dumpfile, "TCP:") {
			hostport := r.dumpfile[4:]
			fd, err = net.Dial("tcp", hostport)
			if err != nil {
				log.Errorf("session recording disabled due to error connecting to host '%s': %s", hostport, err)
				fd = nil
			}
		} else if r.dumpfile == "etcd" {
			fd = nil
		} else {
			fd, err = openRecordFile(r.dumpfile)
			if err != nil {
				log.Errorf("session recording disabled due to error: %s", err)
				fd = nil
			}
		}
		if fd != nil {
			infos := &record.FileInfo{
				Version: 1,
				Time:    r.conninfo.Start,
				SrcIP:   r.conninfo.SSH.SrcIP,
				SrcPort: r.conninfo.SSH.SrcPort,
				DstIP:   r.conninfo.SSH.DstIP,
				DstPort: r.conninfo.SSH.DstPort,
				User:    r.conninfo.User,
				Command: r.command,
			}
			r.writer, err = record.NewWriter(fd, infos)
			if err != nil {
				log.Errorf("session recording disabled due to error: %s", err)
				fd.Close()
				fd = nil
			}
		}
	}
	defer func() {
		r.log(ctx, "final step")
		if fd != nil {
			fd.Close()
		}
	}()

	r.start = time.Now()

	if r.logStatsInterval != 0 {
		go func() {
			for {
				select {
				case <-time.After(r.logStatsInterval):
					r.log(ctx, "intermediate step")
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	bw := uint64(0)
	bwBuf := uint64(0)
	var bwTimeout <-chan time.Time
	if r.dumpLimitWindow != 0 {
		bwTimeout = time.After(r.dumpLimitWindow)
	}
	if r.etcdStatsInterval != 0 {
		go func() {
			time.Sleep(time.Second)
			for {
				select {
				case <-time.After(r.etcdStatsInterval):
					r.updateStats(cli, etcdPath)
				case <-ctx.Done():
					return
				}
			}
		}()
		buf := map[int]uint64{0: 0, 1: 0, 2: 0}
		timeout := time.After(r.etcdStatsInterval)
		for {
			select {
			case <-timeout:
				timeout = time.After(r.etcdStatsInterval)
				r.lock.Lock()
				for i := 0; i <= 2; i++ {
					r.bandwidth[i] = buf[i] / uint64(r.etcdStatsInterval.Seconds())
					buf[i] = 0
				}
				r.lock.Unlock()
			case <-bwTimeout:
				bwTimeout = time.After(r.dumpLimitWindow)
				bw = bwBuf
				bwBuf = 0
			case rec := <-r.ch:
				buf[rec.Fd] += uint64(rec.Size)
				r.lock.Lock()
				r.totals[rec.Fd] += uint64(rec.Size)
				r.lock.Unlock()
				if r.writer != nil {
					if r.dumpLimitSize == 0 || (bw < r.dumpLimitSize && bwBuf < r.dumpLimitSize) {
						r.dump(rec)
					} else if r.dumpLimitWindow == 0 {
						fd.Close()
						fd = nil
						r.writer = nil
					}
					bwBuf += uint64(rec.Size)
				}
			case <-ctx.Done():
				return
			}
		}
	} else {
		for {
			select {
			case <-bwTimeout:
				bwTimeout = time.After(r.dumpLimitWindow)
				bw = bwBuf
				bwBuf = 0
			case rec := <-r.ch:
				r.lock.Lock()
				r.totals[rec.Fd] += uint64(rec.Size)
				r.lock.Unlock()
				if r.writer != nil {
					if r.dumpLimitSize == 0 || (bw < r.dumpLimitSize && bwBuf < r.dumpLimitSize) {
						r.dump(rec)
					} else if r.dumpLimitWindow == 0 {
						fd.Close()
						fd = nil
						r.writer = nil
					}
					bwBuf += uint64(rec.Size)
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

// openRecordFile opens a record file, creating missing subdirectories if
// missing.
func openRecordFile(filename string) (*os.File, error) {
	err := os.MkdirAll(path.Dir(filename), 0700)
	if err != nil {
		return nil, fmt.Errorf("creating directory %s: %s", path.Dir(filename), err)
	}

	f, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("creating %s: %s", filename, err)
	}

	return f, nil
}
