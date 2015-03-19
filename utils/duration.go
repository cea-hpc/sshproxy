package utils

import "time"

type Duration time.Duration

func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var text string
	if err := unmarshal(&text); err != nil {
		return err
	}

	td, err := time.ParseDuration(text)
	if err != nil {
		return err
	}
	*d = Duration(td)
	return nil
}

func (d *Duration) Duration() time.Duration {
	return time.Duration(*d)
}
