package controller

import (
	"os"

	"github.com/agentstax/vulkan/pkg/common"
)

type ControllerConfig struct {
	Logger common.Logger // pass your own *slog.Logger or anything satisfying common.Logger. Default: text lines to stderr, warn level and up.
}

func (c *ControllerConfig) WithDefaults() *ControllerConfig {
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stderr)
	}
	c.Logger = common.BufferLogger(c.Logger)
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ControllerConfig) Validate() error {
	return nil
}
