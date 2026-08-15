package controller

import (
	"errors"
)

// SystemConfig is RegisterSystem's spec -- control-plane settings scoped to no
// single topic. No settings exist today; the surface stays for future
// system-wide knobs.
type SystemConfig struct{}

func (c *SystemConfig) WithDefaults() *SystemConfig {
	return c
}

func (c *SystemConfig) Validate() error {
	return nil
}

// AlterSystemConfig is AlterSystem's sparse patch -- a nil field means leave
// unchanged.
type AlterSystemConfig struct{}

func (c *AlterSystemConfig) Validate() error {
	return errors.New("no fields set -- an alter must change at least one field")
}
