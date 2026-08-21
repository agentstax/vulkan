package controller

import (
	"os"

	"github.com/agentstax/vulkan/pkg/common/logging"
)

type ControllerConfig struct {
	Logger logging.Logger // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
}

func (c *ControllerConfig) WithDefaults() *ControllerConfig {
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.NewPipelineLogger(c.Logger, &logging.PipelineLoggerConfig{Buffer: true})
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ControllerConfig) Validate() error {
	return nil
}
