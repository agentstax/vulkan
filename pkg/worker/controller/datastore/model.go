package datastore

import (
	"github.com/jackc/pgx/v5/pgtype"
)

// WorkerData models the worker_config table row exactly.
type WorkerData struct {
	Id              int64  `db:"id"`
	SystemId        *int64 `db:"system_id"`
	TopicId         *int64 `db:"topic_id"`
	ConsumerGroupId *int64 `db:"consumer_group_id"`
	Name            string `db:"name"`
	Metadata        any    `db:"metadata"` // pgx encodes to and decodes from JSONB
	TargetInstances int    `db:"target_instances"`
}

// ListWorkersData is one row of ListWorkers' query: the worker row plus the
// owner columns joined from topic and consumer_group.
type ListWorkersData struct {
	WorkerData
	OwnerSystemId int64  `db:"owner_system_id"` // system_id resolved through the topic when the row's own is NULL
	OwnerTopicId  int64  `db:"owner_topic_id"`  // through the group for group-owned rows
	TopicName     string `db:"topic_name"`
	ConsumerGroup string `db:"consumer_group"`
}

type WorkerInstanceData struct {
	Id       int64       `db:"id"`
	WorkerId int64       `db:"worker_id"`
	Token    pgtype.UUID `db:"token"`
	Attempts int         `db:"attempts"`
}
