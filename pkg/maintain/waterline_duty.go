package maintain

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
)

// WaterlineRoller runs one consumer group's waterline duty:
// - periodically rolling cursor.committed up behind the group's resolved work.
// Scoped to (topic, group) -- one effective roller per cursor.
type WaterlineRoller struct {
	Datastore *MaintenanceDatastore
	Config    *MaintainerConfig
	Logger    logger.Logger // copied from Config.Logger at construction

	owner *common.Owner // set by Register from the offered maintenance row
	duty  *dutyRunner
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewWaterlineRoller(ds *datastore.PostgresDatastore, cfg *MaintainerConfig) (*WaterlineRoller, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &MaintainerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	maintenanceDatastore, err := NewMaintenanceDatastore(ds, &MaintenanceDatastoreConfig{
		Logger:    cfg.Logger,
		Retry:     cfg.Retry,
		DutyRetry: cfg.DutyRetry,
	})
	if err != nil {
		return nil, err
	}

	return &WaterlineRoller{
		Datastore: maintenanceDatastore,
		Config:    cfg,
		Logger:    cfg.Logger,
	}, nil
}

// shouldRegister reports whether this duty runs the passed duty kind.
func (w *WaterlineRoller) shouldRegister(duty string) bool {
	return duty == DutyWaterline
}

// Register takes the offered row's identity -- the roll needs only the
// owner's topic and group ids. (false, nil) declines a row of another kind.
func (w *WaterlineRoller) Register(ctx context.Context, duty string, owner *common.Owner, meta *DutyMetadata) (bool, error) {
	if !w.shouldRegister(duty) {
		return false, nil
	}
	if w.duty != nil {
		return false, errors.New("waterline roller already registered")
	}
	if owner == nil {
		return false, errors.New("owner must not be nil")
	}
	if meta == nil {
		return false, errors.New("metadata must not be nil")
	}

	if err := migrate.AssertSchemaSupported(ctx, w.Datastore.Datastore.Pool, owner.SystemId, owner.TopicId); err != nil {
		return false, err
	}

	runner, err := newDutyRunner(w.Datastore, w.Logger, w.Config.JitterFraction, DutyWaterline, owner, meta.PollRate)
	if err != nil {
		return false, err
	}

	w.owner = owner
	w.duty = runner
	return true, nil
}

// Run ticks the waterline duty until ctx cancels; a requested stop returns nil.
func (w *WaterlineRoller) Run(ctx context.Context) error {
	if w.duty == nil {
		return errors.New("waterline roller not registered -- call Register first")
	}

	w.Logger.InfoContext(ctx, "waterline duty loop starting", "topic", w.owner.TopicId, "group", w.owner.Name)

	err := w.duty.run(ctx, w.roll)
	if errors.Is(err, context.Canceled) {
		w.Logger.InfoContext(ctx, "waterline roller stopped", "topic", w.owner.TopicId, "group", w.owner.Name)
		return nil
	}
	return err
}

func (w *WaterlineRoller) roll(ctx context.Context) error {
	_, err := w.Datastore.AdvanceWaterline(ctx, w.owner.TopicId, w.owner.ConsumerGroupId)
	return err
}
