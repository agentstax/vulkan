package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	"github.com/agentstax/vulkan/pkg/producer"
)

// AlertController is the alert domain's write path: it records what a run
// found to the __system.alerts topic and logs status changes.
type AlertController struct {
	Logger common.Logger

	alerts *producer.ProducerInstance[alert.Alert]
	heads  *compactioncontroller.CompactionController[alert.Alert]
	repeat time.Duration
}

// alerts is a registered producer instance on the __system.alerts topic;
// heads reads that topic's compaction heads;
// repeat is the alert worker row's repeat_interval.
// ctx is for the clamp warning only. cfg may be nil or a sparse struct --
// WithDefaults fills every field left unset, Validate rejects what's out of
// range.
func NewAlertController(ctx context.Context, alerts *producer.ProducerInstance[alert.Alert], heads *compactioncontroller.CompactionController[alert.Alert], repeat time.Duration, cfg *ControllerConfig) (*AlertController, error) {
	if alerts == nil {
		return nil, errors.New("alert producer instance must not be nil")
	}
	if heads == nil {
		return nil, errors.New("compaction controller must not be nil")
	}
	if repeat <= 0 {
		return nil, fmt.Errorf("repeat must be > 0, got %v", repeat)
	}
	if cfg == nil {
		cfg = &ControllerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// alert repeat needs to be less than retention ttl otherwise could sweep
	// alert head and fake repeat early
	retention := alerts.Topic.RetentionTTL
	if retention > 0 && repeat >= retention {
		clamped := retention / 2
		cfg.Logger.WarnContext(ctx, "alert repeat interval at or above the alerts topic's retention -- clamped",
			"repeat", repeat, "retention", retention, "clamped", clamped)
		repeat = clamped
	}
	return &AlertController{Logger: cfg.Logger, alerts: alerts, heads: heads, repeat: repeat}, nil
}
