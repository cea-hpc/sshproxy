package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"time"

	"sshproxy/record"
	"sshproxy/utils"
)

var SSHPROXY_VERSION string

var (
	versionFlag = flag.Bool("version", false, "show version number and exit")
	listenAddr  = flag.String("listen", ":5555", "listen on this address ([host]:port)")
	outputDir   = flag.String("output", "", "output directory where dumps will be written")
)

func acquire(c net.Conn) {
	defer c.Close()

	addr := c.RemoteAddr()
	log.Printf("[%s] connected", addr)
	defer log.Printf("[%s] disconnected", addr)

	reader := bufio.NewReader(c)
	infos, err := record.ReadHeader(reader)
	if err != nil {
		log.Printf("[%s] error: reading reader: %s\n", addr, err)
		return
	}

	outdir := path.Join(*outputDir, infos.User)
	if err := os.MkdirAll(outdir, 0700); err != nil {
		log.Printf("[%s] error: mkdir '%s': %s", addr, outdir, err)
		return
	}

	fn := fmt.Sprintf("%s-%s.dump", infos.Time.Format(time.RFC3339Nano), utils.CalcSessionId(infos.User, infos.Time, infos.Src()))
	dump := path.Join(outdir, fn)

	f, err := os.Create(dump)
	if err != nil {
		log.Printf("[%s] error: creating '%s': %s", addr, dump, err)
		return
	}
	defer f.Close()

	if err := record.WriteHeader(f, infos); err != nil {
		log.Printf("[%s] error writing header: %s", addr, err)
		return
	}

	if _, err := io.Copy(f, c); err != nil {
		log.Printf("[%s] error copying records: %s", addr, err)
	}
}

func main() {
	flag.Parse()

	if *versionFlag {
		fmt.Fprintf(os.Stderr, "sshproxy-dumpd version %s\n", SSHPROXY_VERSION)
		os.Exit(0)
	}

	if *outputDir == "" {
		log.Fatalf("error: no output directory specified\n")
	}

	l, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("error: listening: %s\n", err)
	}
	defer l.Close()

	log.Printf("listening on %s\n", *listenAddr)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatalf("error: accepting connection: %s\n", err)
		}

		go acquire(conn)
	}
}
