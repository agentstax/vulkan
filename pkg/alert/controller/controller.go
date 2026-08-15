package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/producer"
)

// AlertController is the alert domain's write door: it records what a run
// found to the __system.alerts topic and logs status changes.
type AlertController struct {
	alerts *producer.ProducerInstance[alert.Alert]
	heads  *compactioncontroller.CompactionController[alert.Alert]
	repeat time.Duration
	logger logger.Logger
}

// alerts is a registered producer instance on the __system.alerts topic;
// heads reads that topic's compaction heads;
// repeat is the alert worker row's repeat_interval.
func NewAlertController(ctx context.Context, alerts *producer.ProducerInstance[alert.Alert], heads *compactioncontroller.CompactionController[alert.Alert], repeat time.Duration, log logger.Logger) (*AlertController, error) {
	if alerts == nil {
		return nil, errors.New("alert producer instance must not be nil")
	}
	if heads == nil {
		return nil, errors.New("compaction controller must not be nil")
	}
	if repeat <= 0 {
		return nil, fmt.Errorf("repeat must be > 0, got %v", repeat)
	}
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}

	// alert repeat needs to be less than retention ttl otherwise could sweep
	// alert head and fake repeat early
	retention := alerts.Topic.RetentionTTL
	if retention > 0 && repeat >= retention {
		clamped := retention / 2
		log.WarnContext(ctx, "alert repeat interval at or above the alerts topic's retention -- clamped",
			"repeat", repeat, "retention", retention, "clamped", clamped)
		repeat = clamped
	}
	return &AlertController{alerts: alerts, heads: heads, repeat: repeat, logger: log}, nil
}
