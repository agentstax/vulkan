package datastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/jackc/pgx/v5"
)

// RegisterWorker creates the (name, owner) worker row, or writes metadata onto
// the existing one -- the newest declaration wins. targetInstances is set at
// creation only: 0 is how a worker is suspended, and a redeclaration would
// resume it.
func (d *WorkerDatastore) RegisterWorker(ctx context.Context, name string, owner *common.Owner, metadata any, targetInstances int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.registerWorker(ctx, name, owner, metadata, targetInstances)
	})
}

func (d *WorkerDatastore) registerWorker(ctx context.Context, name string, owner *common.Owner, metadata any, targetInstances int) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// three partial unique indexes cover the owner columns, so no single
	// ON CONFLICT target names the one this row lands on
	insertSql := `
		INSERT INTO worker (system_id, topic_id, consumer_group_id, name, metadata, target_instances)
		VALUES ($1, $2, $3, $4, COALESCE($5, '{}'::jsonb), $6)
		ON CONFLICT DO NOTHING
		RETURNING id;
	`
	var createdId int64
	err = tx.QueryRow(ctx, insertSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name, metadata, targetInstances).Scan(&createdId)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		d.Logger.InfoContext(ctx, "worker declared (created)", "worker", name, "owner", owner.Name, "worker_id", createdId)
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// the stored alias reads the row as it was before the SET, so the same
	// statement writes the declaration and returns both sides of the change
	updateSql := `
		UPDATE worker w
		SET metadata = COALESCE($5, '{}'::jsonb)
		FROM worker stored
		WHERE stored.id = w.id
			AND w.name = $4
			AND w.system_id IS NOT DISTINCT FROM $1
			AND w.topic_id IS NOT DISTINCT FROM $2
			AND w.consumer_group_id IS NOT DISTINCT FROM $3
		RETURNING
			stored.metadata IS DISTINCT FROM w.metadata,
			stored.metadata,
			w.metadata;
	`
	var changed bool
	var storedMetadata, declaredMetadata json.RawMessage
	err = tx.QueryRow(ctx, updateSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name, metadata).
		Scan(&changed, &storedMetadata, &declaredMetadata)
	if errors.Is(err, pgx.ErrNoRows) {
		return worker.ErrDeclarationInterrupted.With("worker", name)
	}
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if !changed {
		d.Logger.InfoContext(ctx, "worker declared (already existed)", "worker", name, "owner", owner.Name)
		return nil
	}

	// the only signal that two services declare this worker differently
	d.Logger.InfoContext(ctx, "worker declared (config replaced)", "worker", name, "owner", owner.Name,
		"metadata", replaced(string(storedMetadata), string(declaredMetadata)))
	return nil
}

// ListWorkers lists the worker rows owned anywhere on owner's chain; a
// system owner also reaches every row below it.
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
	// or all workers if owner is system.
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
			OR w.consumer_group_id = $3
			-- if owner is system we want every worker
			OR ($2 = 0 AND $3 = 0 AND t.system_id = $1);
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
// declared.
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
		return nil, fmt.Errorf("worker %q has no worker row -- the owner's register declares it", name)
	}
	if err != nil {
		return nil, err
	}
	return &data, nil
}

// ***************
// *** HELPERS ***
// ***************

// replaced renders the change as the log line carries it: old -> new. Both
// sides come back from the same statement, so jsonb has normalized both.
func replaced(stored string, declared string) string {
	return fmt.Sprintf("%v -> %v", stored, declared)
}
