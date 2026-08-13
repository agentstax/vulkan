package controller

import (
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
// repeat is the system row's AlertRepeatInterval.
func NewAlertController(alerts *producer.ProducerInstance[alert.Alert], heads *compactioncontroller.CompactionController[alert.Alert], repeat time.Duration, log logger.Logger) (*AlertController, error) {
	if alerts == nil {
		return nil, errors.New("alert producer instance must not be nil")
	}
	if heads == nil {
		return nil, errors.New("compaction controller must not be nil")
	}
	if repeat <= 0 {
		return nil, fmt.Errorf("repeat must be > 0, got %v", repeat)
	}
	// an active head must repeat before retention sweeps it -- checked against
	// the registration default; a live topic altered below repeat is out of scope
	if retention := alert.TopicConfig().RetentionTTL; repeat >= retention {
		return nil, fmt.Errorf("repeat must be < the %s topic's retention %v, got %v", alert.TopicName, retention, repeat)
	}
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}
	return &AlertController{alerts: alerts, heads: heads, repeat: repeat, logger: log}, nil
}
