// Package record provides a representation for the data read from or written
// to a file descriptor with functions to serialize/unserialized it.
//
// The binary representation of a recording is:
// - an unsigned 64 bits integer indicating the received time (in ns),
// - an unsigned 8 bits integer indicating the file descritor,
// - an unsigned 32 bits integer indicating the data size,
// - data.
//
// All integers are big endian.
package record

import (
	"encoding/binary"
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
