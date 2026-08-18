package migrate

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type ControllerConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // transient-error retry policy for the migrate datastore's own Postgres calls. Default: retry.NewDefaultRetryPolicy().
}

func (c *ControllerConfig) WithDefaults() *ControllerConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ControllerConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
