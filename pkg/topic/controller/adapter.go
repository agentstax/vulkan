package controller

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
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

func toAlterTopicData(cfg *AlterTopicConfig) *datastore.AlterTopicData {
	defaults := (&TopicConfig{}).WithDefaults()
	return &datastore.AlterTopicData{
		RetentionTTLNs:         durationNs(toAlterValue(cfg.RetentionTTL, defaults.RetentionTTL)),
		AllowDropPastCommitted: toAlterValue(cfg.AllowDropPastCommitted, defaults.AllowDropPastCommitted),
		IdempotencyKeyTTLNs:    durationNs(toAlterValue(cfg.IdempotencyKeyTTL, defaults.IdempotencyKeyTTL)),
		DeliveryLogMode:        deliveryLogModeString(toAlterValue(cfg.DeliveryLogMode, defaults.DeliveryLogMode)),
	}
}

// toAlterValue flattens one field's Update to the pointer the COALESCE patch
// takes: unchanged -> nil (keep the column), set -> the value, unset -> the
// field's default.
func toAlterValue[T any](update common.Update[T], defaultValue T) *T {
	if value, ok := update.Value(); ok {
		return &value
	}
	if update.IsUnset() {
		return &defaultValue
	}
	return nil
}

func deliveryLogModeEnum(deliveryLogMode string) (topic.DeliveryLogMode, error) {
	switch topic.DeliveryLogMode(deliveryLogMode) {
	case topic.DeliveryLogModeOff, topic.DeliveryLogModeFailures, topic.DeliveryLogModeAll:
		return topic.DeliveryLogMode(deliveryLogMode), nil
	default:
		return "", fmt.Errorf("stored delivery_log_mode %q is not %q, %q, or %q", deliveryLogMode, topic.DeliveryLogModeOff, topic.DeliveryLogModeFailures, topic.DeliveryLogModeAll)
	}
}

func deliveryLogModeString(deliveryLogMode *topic.DeliveryLogMode) *string {
	if deliveryLogMode == nil {
		return nil
	}
	s := string(*deliveryLogMode)
	return &s
}

// durationNs widens *time.Duration to the *int64 the _ns columns store,
// passing nil through so COALESCE sees NULL.
func durationNs(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	ns := int64(*d)
	return &ns
}
