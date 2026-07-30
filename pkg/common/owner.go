package common

import "fmt"

type OwnerKind string

const (
	OwnerSystem        OwnerKind = "system"
	OwnerTopic         OwnerKind = "topic"
	OwnerConsumerGroup OwnerKind = "consumer_group"
)

// Owner is which resource owns a row in a polymorphic table (maintenance,
// migration_log). Zero value = system-owned.
type Owner struct {
	SystemId        int64
	TopicId         int64
	ConsumerGroupId int64
	Name            string // diagnostics only, "" = unnamed
}

func NewSystemOwner() Owner {
	return Owner{Name: "system"}
}

func NewTopicOwner(topicId int64, name string) (Owner, error) {
	if topicId <= 0 {
		return Owner{}, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	return Owner{TopicId: topicId, Name: name}, nil
}

func NewConsumerGroupOwner(topicId int64, consumerGroupId int64, name string) (Owner, error) {
	if topicId <= 0 {
		return Owner{}, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if consumerGroupId <= 0 {
		return Owner{}, fmt.Errorf("consumerGroupId must be > 0, got %d", consumerGroupId)
	}
	return Owner{TopicId: topicId, ConsumerGroupId: consumerGroupId, Name: name}, nil
}

func (o Owner) Kind() OwnerKind {
	switch {
	case o.ConsumerGroupId > 0:
		return OwnerConsumerGroup
	case o.TopicId > 0:
		return OwnerTopic
	default:
		return OwnerSystem
	}
}

// the value stored in the table's topic_id column: id or NULL
func (o Owner) TopicIdColumn() *int64 {
	if o.Kind() == OwnerTopic {
		return &o.TopicId
	}
	return nil
}

// the value stored in the table's consumer_group_id column: id or NULL
func (o Owner) ConsumerGroupIdColumn() *int64 {
	if o.Kind() == OwnerConsumerGroup {
		return &o.ConsumerGroupId
	}
	return nil
}
