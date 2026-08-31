package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/topic/controller/datastore"
)

func toTopicData(data *datastore.TopicConfigRow) (*topic.TopicData, error) {
	deliveryLogMode, err := deliveryLogModeEnum(data.DeliveryLogMode)
	if err != nil {
		return nil, err
	}

	return &topic.TopicData{
		Id:                     data.Id,
		SystemId:               data.SystemId,
		Name:                   data.Name,
		PartitionSize:          data.PartitionSize,
		RetentionTTL:           time.Duration(data.RetentionTTLNs),
		AllowDropPastCommitted: data.AllowDropPastCommitted,
		IdempotencyKeyTTL:      time.Duration(data.IdempotencyKeyTTLNs),
		DeliveryLogMode:        deliveryLogMode,
	}, nil
}

func toTopicConfigRow(systemId int64, name string, cfg *TopicConfig) *datastore.TopicConfigRow {
	return &datastore.TopicConfigRow{
		SystemId:               systemId,
		Name:                   name,
		PartitionSize:          cfg.PartitionSize,
		RetentionTTLNs:         int64(cfg.RetentionTTL),
		AllowDropPastCommitted: cfg.AllowDropPastCommitted,
		IdempotencyKeyTTLNs:    int64(cfg.IdempotencyKeyTTL),
		DeliveryLogMode:        string(cfg.DeliveryLogMode),
	}
}

func deliveryLogModeEnum(deliveryLogMode string) (topic.DeliveryLogMode, error) {
	switch topic.DeliveryLogMode(deliveryLogMode) {
	case topic.DeliveryLogModeOff, topic.DeliveryLogModeFailures, topic.DeliveryLogModeAll:
		return topic.DeliveryLogMode(deliveryLogMode), nil
	default:
		return "", fmt.Errorf("stored delivery_log_mode %q is not %q, %q, or %q", deliveryLogMode, topic.DeliveryLogModeOff, topic.DeliveryLogModeFailures, topic.DeliveryLogModeAll)
	}
}
