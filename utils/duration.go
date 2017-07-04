// Copyright 2015-2017 CEA/DAM/DIF
//  Contributor: Arnaud Guignard <arnaud.guignard@cea.fr>
//
// This software is governed by the CeCILL-B license under French law and
// abiding by the rules of distribution of free software.  You can  use,
// modify and/ or redistribute the software under the terms of the CeCILL-B
// license as circulated by CEA, CNRS and INRIA at the following URL
// "http://www.cecill.info".

package utils

import "time"

type Duration time.Duration

func ParseDuration(text string) (Duration, error) {
	d, err := time.ParseDuration(text)
	if err != nil {
		return Duration(0), err
	}
	return Duration(d), nil
}

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

func (d *Duration) Duration() time.Duration {
	return time.Duration(*d)
}
