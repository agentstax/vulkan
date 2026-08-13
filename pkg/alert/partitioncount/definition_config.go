package partitioncount

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type DefinitionConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // transient-error retry policy for the definition's own Postgres calls. Default: retry.NewDefaultRetryPolicy().

	// InstanceTTL - how long the claimed worker_instance row stays live
	// between heartbeats.
	// Default: 30s.
	InstanceTTL time.Duration
}

func (c *DefinitionConfig) WithDefaults() *DefinitionConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	if c.InstanceTTL == 0 {
		c.InstanceTTL = 30 * time.Second
	}
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *DefinitionConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	if c.InstanceTTL <= 0 {
		return fmt.Errorf("InstanceTTL must be > 0, got %v", c.InstanceTTL)
	}
	return nil
}
