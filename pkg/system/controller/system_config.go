package controller

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
