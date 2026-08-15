package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/topic/controller/datastore"
)

func toTopic(data *datastore.TopicData) (*topic.Topic, error) {
	deliveryLogMode, err := deliveryLogModeEnum(data.DeliveryLogMode)
	if err != nil {
		return nil, err
	}

	return &topic.Topic{
		Id:                     data.Id,
		SystemId:               data.SystemId,
		Name:                   data.Name,
		SchemaVersion:          topic.SchemaVersion(data.SchemaVersion),
		PartitionSize:          data.PartitionSize,
		RetentionTTL:           time.Duration(data.RetentionTTLNs),
		AllowDropPastCommitted: data.AllowDropPastCommitted,
		IdempotencyKeyTTL:      time.Duration(data.IdempotencyKeyTTLNs),
		DeliveryLogMode:        deliveryLogMode,
	}, nil
}

func toRegisterTopicData(systemId int64, name string, version topic.SchemaVersion, cfg *TopicConfig) *datastore.TopicData {
	return &datastore.TopicData{
		SystemId:               systemId,
		Name:                   name,
		SchemaVersion:          int64(version),
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
