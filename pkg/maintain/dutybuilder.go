package maintain

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
)

// dutyBuilder turns one discovered maintenance row into a registered, runnable duty.
type dutyBuilder struct {
	ds     *datastore.PostgresDatastore
	config *FleetMaintainerConfig // jitter/logger/retry feed every built duty's own config
}

func newDutyBuilder(ds *datastore.PostgresDatastore, cfg *FleetMaintainerConfig) *dutyBuilder {
	return &dutyBuilder{
		ds:     ds,
		config: cfg,
	}
}

// build constructs and registers one row's duty.
func (b *dutyBuilder) build(ctx context.Context, key FleetDuty) (Duty, error) {
	cfg := &MaintainerConfig{
		JitterFraction: b.config.JitterFraction,
		Logger:         b.config.Logger,
		Retry:          b.config.Retry,
	}

	var duty Duty
	var err error
	switch key.Duty {
	case DutyJanitor:
		duty, err = NewJanitor(key.TopicName, key.SchemaVersion, b.ds, cfg)
	case DutyWaterline:
		duty, err = NewWaterlineRoller(key.ConsumerGroup, key.TopicName, key.SchemaVersion, b.ds, cfg)
	default:
		err = fmt.Errorf("unknown duty kind %q", key.Duty) // ListDuties filters these; guards direct misuse
	}
	if err == nil {
		err = duty.Register(ctx)
	}
	if err != nil {
		if ctx.Err() == nil {
			b.config.Logger.WarnContext(ctx, "fleet could not spawn duty -- retrying next refresh", "duty", key.Duty, "topic", key.TopicName, "group", key.ConsumerGroup, "error", err)
		}
		return nil, err
	}
	return duty, nil
}
