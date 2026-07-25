package maintain

import (
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type MaintainerConfig struct {
	// JitterFraction spreads claim attempts out of phase: each tick's delay
	// is rate * (1 ± JitterFraction), and the first tick is uniform over one
	// whole interval.
	// Default: 0.1. Must be < 1.
	JitterFraction float64

	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // transient-error retry policy for the maintenance datastore's own Postgres calls. Default: retry.NewDefaultRetryPolicy().
}

func (c *MaintainerConfig) WithDefaults() *MaintainerConfig {
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MaintainerConfig) Validate() error {
	if c.JitterFraction < 0 || c.JitterFraction >= 1 {
		return fmt.Errorf("JitterFraction must be in [0, 1), got %v", c.JitterFraction)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}

type FleetMaintainerConfig struct {
	// PollRate is the discovery cadence: how often the fleet refreshes the
	// duty set and spawns/stops runners to match. Duties claim at their own
	// topics' rates -- this only bounds how promptly a new or removed duty
	// row is noticed.
	// Default: 1s.
	PollRate time.Duration

	// JitterFraction spreads discovery ticks out of phase across fleet
	// replicas, same as MaintainerConfig's.
	// Default: 0.1. Must be < 1.
	JitterFraction float64

	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // transient-error retry policy for the maintenance datastore's own Postgres calls. Default: retry.NewDefaultRetryPolicy().
}

func (c *FleetMaintainerConfig) WithDefaults() *FleetMaintainerConfig {
	if c.PollRate == 0 {
		c.PollRate = time.Second
	}
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *FleetMaintainerConfig) Validate() error {
	if c.PollRate <= 0 {
		return fmt.Errorf("PollRate must be > 0, got %v", c.PollRate)
	}
	if c.JitterFraction < 0 || c.JitterFraction >= 1 {
		return fmt.Errorf("JitterFraction must be in [0, 1), got %v", c.JitterFraction)
	}
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}

type MaintenanceDatastoreConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // transient-error retry policy for this datastore's own Postgres calls. Default: retry.NewDefaultRetryPolicy().
}

func (c *MaintenanceDatastoreConfig) WithDefaults() *MaintenanceDatastoreConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

// Validate runs after WithDefaults -- anything still out of range here was
// set by the caller, not left unset.
func (c *MaintenanceDatastoreConfig) Validate() error {
	if err := c.Retry.Validate(); err != nil {
		return fmt.Errorf("Retry: %w", err)
	}
	return nil
}
