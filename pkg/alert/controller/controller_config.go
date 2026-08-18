package controller

import (
	"os"

	"github.com/agentstax/vulkan/pkg/common"
)

type ControllerConfig struct {
	Logger common.Logger // pass your own *slog.Logger (own Handler) or anything satisfying common.Logger. Default: text logger to stdout, warn level and up.
}

func (c *ControllerConfig) WithDefaults() *ControllerConfig {
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stdout)
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ControllerConfig) Validate() error {
	return nil
}
