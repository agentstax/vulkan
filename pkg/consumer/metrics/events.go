package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/metrics"
)

type GoRoutineEvent struct {
	EventType metrics.EventType `json:"type"`
	TopicId   int64             `json:"topic_id"`
	Group     string            `json:"group"`
	MessageId int64             `json:"message_id"`
	Attempt   int               `json:"attempt"`
	At        time.Time         `json:"at"` // needed for accurate timing with draining behavior
}

func NewGoRoutineEvent(eventType metrics.EventType, topicId int64, group string, messageId int64, attempt int, at time.Time) *GoRoutineEvent {
	return &GoRoutineEvent{
		EventType: eventType,
		TopicId:   topicId,
		Group:     group,
		MessageId: messageId,
		Attempt:   attempt,
		At:        at,
	}
}
