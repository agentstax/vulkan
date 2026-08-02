package datastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/jackc/pgx/v5"
)

// WorkerData models the worker table row exactly.
type WorkerData struct {
	Id              int64
	SystemId        *int64
	TopicId         *int64
	ConsumerGroupId *int64
	Name            string
	Metadata        json.RawMessage
	TargetInstances int
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

// ListWorkers lists every worker seeded in the worker table.
func (d *WorkerDatastore) ListWorkers(ctx context.Context) ([]ListWorkersData, error) {
	var workers []ListWorkersData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		workers, err = d.listWorkers(ctx)
		return err
	})
	return workers, err
}

func (d *WorkerDatastore) listWorkers(ctx context.Context) ([]ListWorkersData, error) {
	sql := `
		SELECT w.id, w.system_id, w.topic_id, w.consumer_group_id, w.name, w.metadata, w.target_instances,
			COALESCE(w.system_id, t.system_id, 0), COALESCE(t.id, 0), COALESCE(t.name, ''), COALESCE(g.name, '')
		FROM worker w
		LEFT JOIN consumer_group g ON g.id = w.consumer_group_id
		LEFT JOIN topic t ON t.id = COALESCE(w.topic_id, g.topic_id);
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []ListWorkersData
	for rows.Next() {
		var data ListWorkersData
		if err := rows.Scan(&data.Id, &data.SystemId, &data.TopicId, &data.ConsumerGroupId, &data.Name, &data.Metadata, &data.TargetInstances,
			&data.OwnerSystemId, &data.OwnerTopicId, &data.TopicName, &data.ConsumerGroup); err != nil {
			return nil, err
		}
		workers = append(workers, data)
	}
	return workers, rows.Err()
}

// GetWorkerMetadata reads the (name, owner) row's metadata. Errors if the
// row was never seeded.
func (d *WorkerDatastore) GetWorkerMetadata(ctx context.Context, name string, owner *common.Owner) (json.RawMessage, error) {
	var raw json.RawMessage
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		raw, err = d.getWorkerMetadata(ctx, name, owner)
		return err
	})
	return raw, err
}

func (d *WorkerDatastore) getWorkerMetadata(ctx context.Context, name string, owner *common.Owner) (json.RawMessage, error) {
	sql := `
		SELECT metadata
		FROM worker
		WHERE name = $1
			AND system_id IS NOT DISTINCT FROM $2
			AND topic_id IS NOT DISTINCT FROM $3
			AND consumer_group_id IS NOT DISTINCT FROM $4;
	`
	var raw json.RawMessage
	err := d.Datastore.Pool.QueryRow(ctx, sql, name, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("worker %q has no worker row -- the owner's register seeds it", name)
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}
