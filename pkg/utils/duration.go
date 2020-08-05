// Copyright 2015-2020 CEA/DAM/DIF
//  Author: Arnaud Guignard <arnaud.guignard@cea.fr>
//  Contributor: Cyril Servant <cyril.servant@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package utils

import "time"

// Duration is a type used in configuration file to unmarshal a duration from a
// YAML string.
type Duration time.Duration

// ParseDuration parses a YAML string into a Duration or returns an error.
func ParseDuration(text string) (Duration, error) {
	d, err := time.ParseDuration(text)
	if err != nil {
		return Duration(0), err
	}
	return Duration(d), nil
}

// UnmarshalYAML is used by the YAML library to unmarshal a string. See go-yaml
// documentation for details.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var text string
	if err := unmarshal(&text); err != nil {
		return err
	}

	var err error
	*d, err = ParseDuration(text)
	if err != nil {
		return err
	}

	return nil
}

// Duration returns a time.Duration object.
func (d *Duration) Duration() time.Duration {
	return time.Duration(*d)
}
