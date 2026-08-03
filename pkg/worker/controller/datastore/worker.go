package datastore

import (
	"context"
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
	Metadata        any // pgx encodes to and decodes from JSONB
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

// InsertWorker creates the (name, owner) worker row; an existing row is left
// untouched.
func (d *WorkerDatastore) InsertWorker(ctx context.Context, name string, owner *common.Owner, metadata any, targetInstances int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.insertWorker(ctx, name, owner, metadata, targetInstances)
	})
}

func (d *WorkerDatastore) insertWorker(ctx context.Context, name string, owner *common.Owner, metadata any, targetInstances int) error {
	sql := `
		INSERT INTO worker (system_id, topic_id, consumer_group_id, name, metadata, target_instances)
		VALUES ($1, $2, $3, $4, COALESCE($5, '{}'::jsonb), $6)
		ON CONFLICT DO NOTHING;
	`
	_, err := d.Datastore.Pool.Exec(ctx, sql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name, metadata, targetInstances)
	return err
}

// ListWorkers lists the worker rows owned anywhere on owner's chain.
func (d *WorkerDatastore) ListWorkers(ctx context.Context, owner *common.Owner) ([]ListWorkersData, error) {
	var workers []ListWorkersData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		workers, err = d.listWorkers(ctx, owner)
		return err
	})
	return workers, err
}

func (d *WorkerDatastore) listWorkers(ctx context.Context, owner *common.Owner) ([]ListWorkersData, error) {
	// one clause per level of the owner chain
	sql := `
		SELECT
			w.id,
			w.system_id,
			w.topic_id,
			w.consumer_group_id,
			w.name,
			w.metadata,
			w.target_instances,
			COALESCE(w.system_id, t.system_id, 0),
			COALESCE(t.id, 0),
			COALESCE(t.name, ''),
			COALESCE(g.name, '')
		FROM worker w
		LEFT JOIN consumer_group g ON g.id = w.consumer_group_id
		LEFT JOIN topic t ON t.id = COALESCE(w.topic_id, g.topic_id)
		WHERE w.system_id = $1
			OR w.topic_id = $2
			OR w.consumer_group_id = $3;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql, owner.SystemId, owner.TopicId, owner.ConsumerGroupId)
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

// GetWorker reads the (name, owner) worker row. Errors if the row was never
// seeded.
func (d *WorkerDatastore) GetWorker(ctx context.Context, name string, owner *common.Owner) (*WorkerData, error) {
	var workerData *WorkerData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		workerData, err = d.getWorker(ctx, name, owner)
		return err
	})
	return workerData, err
}

func (d *WorkerDatastore) getWorker(ctx context.Context, name string, owner *common.Owner) (*WorkerData, error) {
	sql := `
		SELECT 
			id, 
			system_id, 
			topic_id, 
			consumer_group_id, 
			name, 
			metadata, 
			target_instances
		FROM worker
		WHERE name = $1
			AND system_id IS NOT DISTINCT FROM $2
			AND topic_id IS NOT DISTINCT FROM $3
			AND consumer_group_id IS NOT DISTINCT FROM $4;
	`
	var data WorkerData
	err := d.Datastore.Pool.QueryRow(ctx, sql, name, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn()).
		Scan(&data.Id, &data.SystemId, &data.TopicId, &data.ConsumerGroupId, &data.Name, &data.Metadata, &data.TargetInstances)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("worker %q has no worker row -- the owner's register seeds it", name)
	}
	if err != nil {
		return nil, err
	}
	return &data, nil
}
