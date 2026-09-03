package vulkan

import (
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/datastore"
)

type ClientConfig struct {
	// Schema - the Postgres namespace holding every vulkan table.
	// Default: "vulkan".
	//
	// One schema is one installation: two clients on two schemas in the
	// same database share nothing.
	Schema string

	// AllowDestroy - whether this client may destroy topics at all.
	// Default: false.
	//
	// A service that only ever registers topics should never opt in --
	// create is recoverable, destroy is not.
	AllowDestroy bool

	// DisableManager - whether Consume skips running the system manager
	// beside its session; explicit RunManager calls are unaffected.
	// Default: false.
	//
	// Set it where upkeep must not run in this process: a deployment with
	// dedicated `vulkan manager run` processes, or consumers under a
	// database role without DDL rights (the topic janitor runs DDL).
	DisableManager bool

	Logger logging.Logger      // pass your own *slog.Logger or anything satisfying logging.Logger. Default: text lines to stderr, warn level and up.
	Retry  *common.RetryPolicy // transient-error retry policy for the client's own Postgres calls -- never a message's redelivery. Default: common.NewDefaultRetryPolicy().
}

func (c *ClientConfig) WithDefaults() *ClientConfig {
	if c.Schema == "" {
		c.Schema = datastore.DefaultSchema
	}
	if c.Logger == nil {
		c.Logger = logging.NewDefaultLogger(os.Stderr)
	}
	c.Logger = logging.NewPipelineLogger(c.Logger, &logging.PipelineLoggerConfig{Buffer: true})
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *ClientConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
