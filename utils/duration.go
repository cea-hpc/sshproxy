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
