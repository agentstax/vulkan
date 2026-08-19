package common

import "fmt"

type OwnerKind string

const (
	// OwnerAny lifts an owner-kind guard -- any kind is admitted, like the
	// manager worker every owner declares.
	OwnerAny OwnerKind = ""

	OwnerSystem        OwnerKind = "system"
	OwnerTopic         OwnerKind = "topic"
	OwnerConsumerGroup OwnerKind = "consumer_group"
)

func (k OwnerKind) Validate() error {
	switch k {
	case OwnerSystem, OwnerTopic, OwnerConsumerGroup:
		return nil
	default:
		return fmt.Errorf("unknown owner kind %q", k)
	}
}

// Owner is which resource owns a row in a polymorphic table (worker,
// cron_job, migration_log).
type Owner struct {
	SystemId        int64
	TopicId         int64
	ConsumerGroupId int64
	Name            string
}

func NewSystemOwner(systemId int64) (*Owner, error) {
	if systemId <= 0 {
		return nil, fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	return &Owner{SystemId: systemId, Name: "system"}, nil
}

func NewTopicOwner(systemId int64, topicId int64, name string) (*Owner, error) {
	if systemId <= 0 {
		return nil, fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	return &Owner{SystemId: systemId, TopicId: topicId, Name: name}, nil
}

func NewConsumerGroupOwner(systemId int64, topicId int64, consumerGroupId int64, name string) (*Owner, error) {
	if systemId <= 0 {
		return nil, fmt.Errorf("systemId must be > 0, got %d", systemId)
	}
	if topicId <= 0 {
		return nil, fmt.Errorf("topicId must be > 0, got %d", topicId)
	}
	if consumerGroupId <= 0 {
		return nil, fmt.Errorf("consumerGroupId must be > 0, got %d", consumerGroupId)
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	return &Owner{SystemId: systemId, TopicId: topicId, ConsumerGroupId: consumerGroupId, Name: name}, nil
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

// IdColumns is the owner as a polymorphic table's owner columns: the owning
// id in its kind's column, 0 everywhere else. The Go-side twin of the
// *IdColumn methods, for comparing against scanned rows where NULL reads as 0.
func (o Owner) IdColumns() (systemId int64, topicId int64, consumerGroupId int64) {
	switch o.Kind() {
	case OwnerConsumerGroup:
		return 0, 0, o.ConsumerGroupId
	case OwnerTopic:
		return 0, o.TopicId, 0
	default:
		return o.SystemId, 0, 0
	}
}

// the value stored in the table's system_id column: id or NULL
func (o Owner) SystemIdColumn() *int64 {
	if o.Kind() == OwnerSystem {
		return &o.SystemId
	}
	return nil
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
