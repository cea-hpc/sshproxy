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
	"reflect"
	"testing"
	"unsafe"
)

var goStringTests = []struct {
	input1, input2, want string
}{
	// empty string
	{"", "", ""},
	// string ending with \0
	{"cyril", "", "cyril"},
	// string with a \0 in the middle
	{"test", "cyril", "test"},
}

func TestGoString(t *testing.T) {
	// nil pointer
	got := goString(uintptr(unsafe.Pointer(nil)))
	if got != "" {
		t.Errorf("goString got \"%s\", want \"\"", got)
	}
	// strings
	for _, tt := range goStringTests {
		b := append([]byte(tt.input1), 0)
		b = append(b, []byte(tt.input2)...)
		got := goString(uintptr(unsafe.Pointer(&b[0])))
		if got != tt.want {
			t.Errorf("goString got \"%s\", want \"%s\"", got, tt.want)
		}
	}
}

func BenchmarkGoString(b *testing.B) {
	// nil pointer
	b.Run("nil", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			goString(uintptr(unsafe.Pointer(nil)))
		}
	})
	// strings
	for _, tt := range goStringTests {
		b.Run(tt.input1+"_"+tt.input2, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b := append([]byte(tt.input1), 0)
				b = append(b, []byte(tt.input2)...)
				goString(uintptr(unsafe.Pointer(&b[0])))
			}
		})
	}
}

var InitExpanderTests = []struct {
	ns, err string
	want    []string
}{
	{"@test", "must not contain the @ character", []string{}},
	{"test", "", []string{"test"}},
	{"server[1,3-5]", "", []string{"server1", "server3", "server4", "server5"}},
	{"server[1-2],server4", "", []string{"server1", "server2", "server4"}},
	{"server[1-2", "unbalanced '[' found while parsing server[1-2 - nodeset parse error", []string{}},
}

func TestInitExpander(t *testing.T) {
	got, close_func, expand_func := InitExpander()
	want := "libnodeset.so not found, falling back to iskylite's implementation"
	if got != want {
		t.Errorf("InitExpander got \"%s\", want \"%s\"", got, want)
	} else {
		for _, tt := range InitExpanderTests {
			got, err := expand_func(tt.ns)
			if err == nil && tt.err != "" {
				t.Errorf("got no error, want %s", tt.err)
			} else if err != nil && err.Error() != tt.err {
				t.Errorf("ERROR: %s, want %s", err, tt.err)
			} else if err == nil {
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("got:\n%v\nwant:\n%v", got, tt.want)
				}
			}
		}
		err := close_func()
		if err != nil {
			t.Errorf("InitExpander close function got \"%s\", want nil", err)
		}
	}
}

func BenchmarkInitExpander(b *testing.B) {
	for _, tt := range InitExpanderTests {
		b.Run(tt.ns, func(b *testing.B) {
			_, close_func, expand_func := InitExpander()
			expand_func(tt.ns)
			close_func()
		})
	}
}
