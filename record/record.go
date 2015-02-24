// Package record provides a representation for the data read from or written
// to a file descriptor with functions to serialize/unserialized it.
//
// The binary representation of a recording is:
// - an unsigned 64 bits integer indicating the received time (in ns),
// - an unsigned 8 bits integer indicating the file descritor,
// - an unsigned 32 bits integer indicating the data size,
// - data.
//
// A file record has a header with the following fields:
// - an unsigned 16 bits integer for the version number,
// - a NULL terminated string with the command run by the user (can be empty
// with only its NULL end).
//
// All integers are big endian.
package record

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// A Record represents the data read from or written to a file descriptor.
type Record struct {
	Time time.Time // received time
	Fd   int       // integer file descriptor
	Size int       // size of data
	Data []byte    // data
}

// Binary header of a record
type RecordHeader struct {
	Time uint64
	Fd   uint8
	Size uint32
}

// Decode reads a binary record from an io.Reader and fills the provided
// *Record struct.
//
// It uses a pointer over a *Record struct instead of returning a *Record
// struct for performance reason: it tries to reuse the already allocated Data
// field.
func Decode(rd io.Reader, rec *Record) error {
	var hdr RecordHeader
	if err := binary.Read(rd, binary.BigEndian, &hdr); err != nil {
		return err
	}

	rec.Time = time.Unix(0, int64(hdr.Time))
	rec.Fd = int(hdr.Fd)
	rec.Size = int(hdr.Size)

	// Reuse the data slice
	if cap(rec.Data) >= int(rec.Size) {
		rec.Data = rec.Data[:rec.Size]
	} else {
		rec.Data = make([]byte, rec.Size)
	}

	if _, err := io.ReadFull(rd, rec.Data); err != nil {
		return err
	}

	return nil
}

// Encode writes a *Record struct into its binary representation in the
// provided io.Writer.
func Encode(wd io.Writer, rec *Record) error {
	hdr := RecordHeader{
		uint64(rec.Time.UnixNano()),
		uint8(rec.Fd),
		uint32(rec.Size),
	}

	if err := binary.Write(wd, binary.BigEndian, &hdr); err != nil {
		return err
	}

	if _, err := wd.Write(rec.Data); err != nil {
		return err
	}

	return nil
}

// FileHeader is the parsed header of a record file.
type FileHeader struct {
	Version uint16
	Command string
}

// Reader parses record file.
type Reader struct {
	Header FileHeader
	reader *bufio.Reader
	err    error
}

// NewReader reads records from an io.Reader.
func NewReader(reader io.Reader) (*Reader, error) {
	r := &Reader{
		reader: bufio.NewReader(reader),
	}

	if err := binary.Read(r.reader, binary.BigEndian, &r.Header.Version); err != nil {
		return nil, fmt.Errorf("reading version number: %s", err)
	}

	if r.Header.Version != 1 {
		return nil, fmt.Errorf("Unknow version number: %x", r.Header.Version)
	}

	command, err := r.reader.ReadString(0x0)
	if err != nil {
		return nil, fmt.Errorf("reading command: %s", err)
	}
	// remove trailing \0
	r.Header.Command = command[:len(command)-1]

	return r, nil
}

// Next fills the provided Record with the next record read from the Reader.
func (r *Reader) Next(rec *Record) error {
	return Decode(r.reader, rec)
}

// Writers writes records into a file.
type Writer struct {
	writer io.Writer
}

func NewWriter(writer io.Writer, header *FileHeader) (*Writer, error) {
	w := &Writer{
		writer: writer,
	}

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, header.Version); err != nil {
		return nil, err
	}

	buf.WriteString(header.Command)
	buf.WriteByte(0x0)

	if _, err := buf.WriteTo(w.writer); err != nil {
		return nil, fmt.Errorf("writing file header: %s", err)
	}

	return w, nil
}

func (w *Writer) Write(rec *Record) error {
	return Encode(w.writer, rec)
}
