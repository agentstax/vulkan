package base

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
)

type BaseProvisionerConfig struct {
	Logger common.Logger       // pass your own *slog.Logger (own Handler) or anything satisfying common.Logger. Default: text logger to stdout, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for the provisioner's controllers' own Postgres calls. Default: common.NewDefaultRetryPolicy().
}

func (c *BaseProvisionerConfig) WithDefaults() *BaseProvisionerConfig {
	if c.Logger == nil {
		c.Logger = common.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *BaseProvisionerConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
