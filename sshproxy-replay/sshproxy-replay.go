package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"sshproxy/record"
)

var (
	replayFlag = flag.Bool("replay", false, "live replay a session (as the user did it)")
)

func replay(filename string) {
	fmt.Printf("===> opening %s\n", filename)

	f, err := os.Open(filename)
	if err != nil {
		log.Printf("error reading: %s\n", err)
		return
	}
	defer f.Close()

	bf := bufio.NewReader(f)

	var rec record.Record
	var start, previous time.Time
	var elapsed, direction string
	var stream *os.File
	dayFormat := "Jan 02 15:04:05"
	for {
		err := record.Decode(bf, &rec)
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("error reading: %s\n", err)
			return
		}
		if *replayFlag {
			if !previous.IsZero() {
				time.Sleep(rec.Time.Sub(previous))
			}
			previous = rec.Time
			switch rec.Fd {
			case 0:
				continue
			case 1:
				stream = os.Stdout
			case 2:
				stream = os.Stderr
			}
			stream.Write(rec.Data)
		} else {
			if start.IsZero() {
				start = rec.Time
				elapsed = rec.Time.Format(dayFormat)
			} else {
				elapsed = fmt.Sprintf("+%.6f", rec.Time.Sub(start).Seconds())
			}
			switch rec.Fd {
			case 0:
				direction = "-->"
			case 1:
				direction = "<--"
			case 2:
				direction = "<=="
			}
			fmt.Printf("[%[1]*s] [%s] %d bytes\n", len(dayFormat), elapsed, direction, rec.Size)
			fmt.Println(hex.Dump(rec.Data))
		}
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: sshproxy-replay files ...\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() == 0 {
		usage()
	}

	for _, fn := range flag.Args() {
		replay(fn)
	}
}
