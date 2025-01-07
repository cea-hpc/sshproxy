// Copyright 2015-2025 CEA/DAM/DIF
//  Author: Arnaud Guignard <arnaud.guignard@cea.fr>
//  Contributor: Cyril Servant <cyril.servant@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package nodesets

import (
	"errors"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/iskylite/nodeset"
)

// goString function from github.com/ebitengine/purego/internal
func goString(c uintptr) string {
	// We take the address and then dereference it to trick go vet from
	// creating a possible misuse of unsafe.Pointer
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&c))
	if ptr == nil {
		return ""
	}
	var length int
	for {
		if *(*byte)(unsafe.Add(ptr, uintptr(length))) == '\x00' {
			break
		}
		length++
	}
	return string(unsafe.Slice((*byte)(ptr), length))
}

// InitExpander checks if nodeset-rs/libnodeset.so is available. If it is not
// available, it falls back on iskylite's go implementation.
func InitExpander() (
	string, // Comment for verbose logs
	func() error, // Close function
	func(ns string) ([]string, error)) { // Expand function

	libnodeset, err := purego.Dlopen("libnodeset.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)

	if err != nil {
		return "libnodeset.so not found, falling back to iskylite's implementation", func() error {
				// Close function: do nothing
				return nil
			}, func(ns string) ([]string, error) {
				// Expand function
				if strings.Contains(ns, "@") {
					return nil, errors.New("must not contain the @ character")
				}
				return nodeset.Expand(ns)
			}
	}

	return "libnodeset.so found", func() error {
			// Close function
			return purego.Dlclose(libnodeset)
		}, func(ns string) ([]string, error) {
			// Expand function
			var ns_free_error func(uintptr)
			purego.RegisterLibFunc(&ns_free_error, libnodeset, "ns_free_error")
			var ns_free_nodeset func(uintptr)
			purego.RegisterLibFunc(&ns_free_nodeset, libnodeset, "ns_free_nodeset")
			var ns_free_iter func(uintptr)
			purego.RegisterLibFunc(&ns_free_iter, libnodeset, "ns_free_iter")
			var init_default_resolver func(*uintptr) int
			purego.RegisterLibFunc(&init_default_resolver, libnodeset, "init_default_resolver")
			var ns_parse func(string, *uintptr) uintptr
			purego.RegisterLibFunc(&ns_parse, libnodeset, "ns_parse")
			var ns_count func(uintptr) int
			purego.RegisterLibFunc(&ns_count, libnodeset, "ns_count")
			var ns_iter func(uintptr) uintptr
			purego.RegisterLibFunc(&ns_iter, libnodeset, "ns_iter")
			var ns_iter_next func(uintptr, *uintptr) string
			purego.RegisterLibFunc(&ns_iter_next, libnodeset, "ns_iter_next")
			var ns_iter_status func(uintptr) int
			purego.RegisterLibFunc(&ns_iter_status, libnodeset, "ns_iter_status")
			var err uintptr
			if init_default_resolver(&err) != 0 {
				defer ns_free_error(err)
				return nil, errors.New(goString(err))
			}
			nsParsed := ns_parse(ns, &err)
			if goString(err) != "" {
				defer ns_free_error(err)
				return nil, errors.New(goString(err))
			}
			defer ns_free_nodeset(nsParsed)
			nsIter := ns_iter(nsParsed)
			defer ns_free_iter(nsIter)
			nsCount := ns_count(nsParsed)
			hosts := make([]string, nsCount)
			i := 0
			for {
				nsIterNext := ns_iter_next(nsIter, &err)
				if nsIterNext == "" {
					break
				}
				hosts[i] = nsIterNext
				i++
			}
			if ns_iter_status(nsIter) != 0 {
				defer ns_free_error(err)
				return nil, errors.New(goString(err))
			}
			return hosts, nil
		}
}
