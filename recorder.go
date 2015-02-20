package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"sshproxy/record"
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
	s.ch <- record.Record{time.Now(), s.fd, n, pp}
	return n, err
}

// Write implements the Writer Write method. It sends a copy of the written
// slice to its internal channel.
func (s *Splitter) Write(p []byte) (int, error) {
	pp := Dup(p, len(p))
	s.ch <- record.Record{time.Now(), s.fd, len(p), pp}
	return s.f.Write(p)
}

// A Recorder intercepts data read from standard input and written to standard
// ouput or standard error.
//
// It logs periodically basic statistics of transferred bytes and can save
// intercepted raw data in a file.
//
// The file is a succession of serialized record.Record structs. See the
// record.Record documentation for details on the format.
type Recorder struct {
	Stdin, Stdout, Stderr io.ReadWriteCloser // standard input, output and error to be used instead of the standard file descriptors.
	start                 time.Time          // when the Recorder was started
	stats_interval        duration           // interval at which basic statistics of transferred bytes are logged
	totals                map[int]int        // total of bytes for each recorded file descriptor
	ch                    chan record.Record // channel to read record.Record structs
	fdump                 *os.File           // *os.File where the raw records are dumped.
	done                  <-chan struct{}    // control channel to stop recording when it's closed
}

// NewRecorder returns a new Recorder struct.
//
// If dumpfile is not empty, the intercepted raw data will be written in this
// file. Logging of basic statistics will be done every stats_interval seconds.
// It will stop recording when the done channel is closed.
func NewRecorder(dumpfile string, stats_interval duration, done <-chan struct{}) (*Recorder, error) {
	var fdump *os.File = nil
	if dumpfile != "" {
		err := os.MkdirAll(path.Dir(dumpfile), 0700)
		if err != nil {
			return nil, fmt.Errorf("creating directory %s: %s", path.Dir(dumpfile), err)
		}

		fdump, err = os.Create(dumpfile)
		if err != nil {
			return nil, fmt.Errorf("creating %s: %s", dumpfile, err)
		}
	}

	ch := make(chan record.Record)

	return &Recorder{
		Stdin:          NewSplitter(os.Stdin, ch),
		Stdout:         NewSplitter(os.Stdout, ch),
		Stderr:         NewSplitter(os.Stderr, ch),
		stats_interval: stats_interval,
		totals:         map[int]int{0: 0, 1: 0, 2: 0},
		ch:             ch,
		fdump:          fdump,
		done:           done,
	}, nil
}

// log formats the internal statistics and logs them.
func (r *Recorder) log() {
	t := []string{}
	fds := []string{"stdin", "stdout", "stderr"}
	for fd, name := range fds {
		t = append(t, fmt.Sprintf("%s: %d", name, r.totals[fd]))
	}
	// round to second
	elapsed := time.Duration((time.Now().Sub(r.start) / time.Second) * time.Second)
	log.Notice("bytes transferred in %s: %s", elapsed, strings.Join(t, ", "))
}

// dump saves a record.Record in the dumpfile.
func (r *Recorder) dump(rec record.Record) {
	if r.fdump == nil {
		return
	}

	if err := record.Encode(r.fdump, &rec); err != nil {
		log.Error("writing in %s: %s", r.fdump.Name(), err)
		// XXX close r.fdump?
	}
}

// Run starts the recorder.
func (r *Recorder) Run() {
	defer func() {
		r.fdump.Close()
		r.log()
	}()

	r.start = time.Now()

	if r.stats_interval.Duration != 0 {
		go func() {
			for {
				select {
				case <-time.After(r.stats_interval.Duration):
					r.log()
				case <-r.done:
					return
				}
			}
		}()
	}

	for {
		select {
		case rec := <-r.ch:
			r.totals[rec.Fd] += rec.Size
			if r.fdump != nil {
				r.dump(rec)
			}
		case <-r.done:
			return
		}
	}
}