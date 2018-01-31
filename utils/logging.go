// Copyright 2015-2017 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package utils

import (
	"log"
	"os"
	"path"

	"github.com/op/go-logging"
)

// MustSetupLogging setups logging framework.
//
// logfile can be:
//   - empty (""): logs will be written on stdout,
//   - "syslog": logs will be sent to syslog(),
//   - a filename: logs will be appended in this file (the subdirectories will
//     be created if they do not exist).
//
// module is the module name of the main logger.
// logformat and syslogformat are strings to format message (see go-logging
// documentation for details).
// Debug output is enabled if debug is true.
func MustSetupLogging(module, logfile, logformat, syslogformat string, debug bool) {
	var logBackend logging.Backend
	logFormat := logformat
	if logfile == "syslog" {
		var err error
		logBackend, err = logging.NewSyslogBackend(module)
		if err != nil {
			log.Fatalf("error opening syslog: %s", err)
		}
		logFormat = syslogformat
	} else {
		var f *os.File
		if logfile == "" {
			f = os.Stdout
		} else {
			err := os.MkdirAll(path.Dir(logfile), 0755)
			if err != nil {
				log.Fatalf("creating directory %s: %s", path.Dir(logfile), err)
			}

			f, err = os.OpenFile(logfile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				log.Fatalf("error opening log file %s: %v", logfile, err)
			}
		}
		logBackend = logging.NewLogBackend(f, "", 0)
	}

	logging.SetBackend(logBackend)
	logging.SetFormatter(logging.MustStringFormatter(logFormat))
	if debug {
		logging.SetLevel(logging.DEBUG, module)
	} else {
		logging.SetLevel(logging.NOTICE, module)
	}
}
