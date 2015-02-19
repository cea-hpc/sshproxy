package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// Dup duplicates a []byte slice.
func Dup(a []byte) []byte {
	b := make([]byte, len(a))
	copy(b, a)
	return b
}

// A Record represents the data read from or written to a file descriptor.
type Record struct {
	Fd  int    // integer file descriptor
	Buf []byte // read/written data
}

// A Splitter reads from and/or writes to a file descriptor and sends a Record
// struct to a channel for each read/write operation.
type Splitter struct {
	f  *os.File      // opened file
	fd int           // integer file descriptor
	ch chan<- Record // channel to send Record structs
}

// NewSplitter returns a new Splitter struct from an already opened *os.File
// and a channel where Record structs will be sent.
//
// It implements the ReadWriteCloser interface.
func NewSplitter(f *os.File, ch chan Record) *Splitter {
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
	pp := Dup(p)
	s.ch <- Record{s.fd, pp}
	return n, err
}

// Write implements the Writer Write method. It sends a copy of the written
// slice to its internal channel.
func (s *Splitter) Write(p []byte) (int, error) {
	pp := Dup(p)
	s.ch <- Record{s.fd, pp}
	return s.f.Write(p)
}

// A Recorder intercepts data read from standard input and written to standard
// ouput or standard error.
//
// It logs periodically basic statistics of transferred bytes and can save
// intercepted raw data in a file. The file is a succession of serialized
// Record structs with the following format: an unsigned 8 bits integer
// indicating the file descritor, an unsigned 32 bits integer indicating the
// data size, data. All integers are big endian.
type Recorder struct {
	Stdin, Stdout, Stderr io.ReadWriteCloser // standard input, output and error to be used instead of the standard file descriptors.
	start                 time.Time          // when the Recorder was started
	stats_interval        duration           // interval at which basic statistics of transferred bytes are logged
	totals                map[int]int        // total of bytes for each recorded file descriptor
	ch                    chan Record        // channel to read Record structs
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

	ch := make(chan Record)

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

// dump saves a Record in the dumpfile.
func (r *Recorder) dump(rec Record) {
	if r.fdump == nil {
		return
	}

	buf := new(bytes.Buffer)
	data := []interface{}{
		uint8(rec.Fd),
		uint32(len(rec.Buf)),
	}

	for _, v := range data {
		err := binary.Write(buf, binary.BigEndian, v)
		if err != nil {
			log.Error("binary.Write failed: %s", err)
			return
		}
	}

	_, err := buf.Write(rec.Buf)
	if err != nil {
		log.Error("bytes.Buffer.Write failed: %s", err)
		return
	}

	_, err = buf.WriteTo(r.fdump)
	if err != nil {
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
			r.totals[rec.Fd] += len(rec.Buf)
			if r.fdump != nil {
				r.dump(rec)
			}
		case <-r.done:
			return
		}
	}
}
