package maintain

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
)

// FleetMaintainer is a spawner: it keeps one running Janitor/WaterlineRoller
// per duty row in the maintenance table. Each refresh lists the table and
// reconciles the duty pool against it -- new rows spawn a duty, vanished
// rows (destroyed topic, dropped cursor, changed rate) stop one.
type FleetMaintainer struct {
	Datastore *MaintenanceDatastore
	Config    *FleetMaintainerConfig
	Logger    logger.Logger // copied from Config.Logger at construction

	registered bool
	duties     *dutyPool
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewFleetMaintainer(ds *datastore.PostgresDatastore, cfg *FleetMaintainerConfig) (*FleetMaintainer, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &FleetMaintainerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	maintenanceDatastore, err := NewMaintenanceDatastore(ds, &MaintenanceDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &FleetMaintainer{
		Datastore: maintenanceDatastore,
		Config:    cfg,
		Logger:    cfg.Logger,
		duties:    newDutyPool(cfg.Logger, newDutyBuilder(ds, cfg)),
	}, nil
}

// Register asserts the shared system schema only.
func (f *FleetMaintainer) Register(ctx context.Context) error {
	if f.registered {
		return errors.New("fleet maintainer already registered")
	}

	if err := migrate.AssertSystemSchemaSupported(ctx, f.Datastore.Datastore.Pool); err != nil {
		return err
	}

	f.registered = true
	return nil
}

// Run refreshes the duty set until ctx cancels; a requested stop returns nil.
func (f *FleetMaintainer) Run(ctx context.Context) error {
	if !f.registered {
		return errors.New("fleet maintainer not registered -- call Register first")
	}

	f.Logger.InfoContext(ctx, "fleet maintainer starting")

	// first refresh immediately -- spawned duties already jitter their own
	// first claims, so there's no storm to spread here
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			// every duty's ctx derives from this one, so the pool is already
			// stopping -- wait for the duty claims to drain
			f.duties.wait()
			f.Logger.InfoContext(ctx, "fleet maintainer stopped")
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil // gracefully shutdown, not an error
			}
			return ctx.Err()
		case <-timer.C:
		}

		duties, err := f.Datastore.ListDuties(ctx)
		if err != nil {
			if ctx.Err() == nil {
				f.Logger.ErrorContext(ctx, "fleet duty refresh failed", "error", err)
			}
		} else {
			f.duties.reconcile(ctx, duties)
		}

		// re-jittered every refresh so replicas' phases keep drifting apart
		jitter := 1 + f.Config.JitterFraction*(2*rand.Float64()-1)
		timer.Reset(time.Duration(float64(f.Config.PollRate) * jitter))
	}
}
