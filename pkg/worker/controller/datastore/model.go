package datastore

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// WorkerData models the worker table row exactly.
type WorkerData struct {
	Id              int64
	SystemId        *int64
	TopicId         *int64
	ConsumerGroupId *int64
	Name            string
	Metadata        any // pgx encodes to and decodes from JSONB
	TargetInstances int
}

// WorkerMetadataData is AlterWorkers' working row: the row id plus the
// metadata being rewritten.
type WorkerMetadataData struct {
	Id       int64
	Metadata map[string]any
}

// ListWorkersData is one row of ListWorkers' query: the worker row plus the
// owner columns joined from topic and consumer_group.
type ListWorkersData struct {
	WorkerData
	OwnerSystemId int64 // system_id resolved through the topic when the row's own is NULL
	OwnerTopicId  int64 // through the group for group-owned rows
	TopicName     string
	ConsumerGroup string
}

type WorkerInstanceData struct {
	Id        int64
	WorkerId  int64
	Token     pgtype.UUID
	ExpiresAt time.Time
	Attempts  int
}
