package admin

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
)

type MessageAdminConfig struct {
	// AllowDestroy - whether this admin may destroy topics at all.
	// Default: false.
	//
	// A service that only ever registers topics should never opt in --
	// create is recoverable, destroy is not.
	AllowDestroy bool

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // Default: common.NewDefaultRetryPolicy().
}

func (c *MessageAdminConfig) WithDefaults() *MessageAdminConfig {
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.BufferLogger(c.Logger)
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MessageAdminConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
