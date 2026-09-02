package datastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/jackc/pgx/v5"
)

// RegisterWorker creates the (name, owner) worker row, or writes metadata onto
// the existing one -- the newest declaration wins -- appending a worker_config_log
// snapshot in the same transaction. targetInstances is set at creation
// only: 0 is how a worker is suspended, and a redeclaration would resume it.
func (d *WorkerDatastore) RegisterWorker(ctx context.Context, name string, owner *common.Owner, metadata any, targetInstances int, declaredBy string) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.registerWorker(ctx, name, owner, metadata, targetInstances, declaredBy)
	})
}

func (d *WorkerDatastore) registerWorker(ctx context.Context, name string, owner *common.Owner, metadata any, targetInstances int, declaredBy string) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// three partial unique indexes cover the owner columns, so no single
	// ON CONFLICT target names the one this row lands on
	insertSql := fmt.Sprintf(`
		-- vulkan: worker.registerWorker
		INSERT INTO %[1]s.worker_config (system_id, topic_id, consumer_group_id, name, metadata, target_instances)
		VALUES ($1, $2, $3, $4, COALESCE($5, '{}'::jsonb), $6)
		ON CONFLICT DO NOTHING
		RETURNING id;
	`, d.Datastore.Schema)
	var createdId int64
	err = tx.QueryRow(ctx, insertSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name, metadata, targetInstances).Scan(&createdId)
	if err == nil {
		if err := d.appendWorkerConfigLog(ctx, tx, createdId, declaredBy); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		d.Logger.InfoContext(ctx, "worker declared (created)", "worker", name, "owner", owner.Name, "worker_id", createdId)
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// do metadata comparision in db as it is normalized there
	// if we compared go marshaled bytes we could report false changes
	readSql := fmt.Sprintf(`
		-- vulkan: worker.registerWorker
		SELECT id, metadata, metadata = COALESCE($5, '{}'::jsonb) AS unchanged
		FROM %[1]s.worker_config
		WHERE name = $4
			AND system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3;
	`, d.Datastore.Schema)
	var workerId int64
	var storedMetadata json.RawMessage
	var unchanged bool
	err = tx.QueryRow(ctx, readSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name, metadata).
		Scan(&workerId, &storedMetadata, &unchanged)
	if errors.Is(err, pgx.ErrNoRows) {
		return worker.ErrDeclarationInterrupted.With("worker", name)
	}
	if err != nil {
		return err
	}
	if unchanged {
		d.Logger.InfoContext(ctx, "worker declared (already existed)", "worker", name, "owner", owner.Name)
		return nil
	}

	updateSql := fmt.Sprintf(`
		-- vulkan: worker.registerWorker
		UPDATE %[1]s.worker_config
		SET metadata = COALESCE($2, '{}'::jsonb)
		WHERE id = $1
		RETURNING metadata;
	`, d.Datastore.Schema)
	var declaredMetadata json.RawMessage
	err = tx.QueryRow(ctx, updateSql, workerId, metadata).Scan(&declaredMetadata)
	if errors.Is(err, pgx.ErrNoRows) {
		return worker.ErrDeclarationInterrupted.With("worker", name)
	}
	if err != nil {
		return err
	}

	if err := d.appendWorkerConfigLog(ctx, tx, workerId, declaredBy); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, worker.EventWorkerConfigReplaced.Message, "code", worker.EventWorkerConfigReplaced.Code,
		"worker", name, "worker_id", workerId, "owner", owner.Name,
		"metadata", replaced(string(storedMetadata), string(declaredMetadata)))
	return nil
}

// appendWorkerConfigLog writes the worker row's full snapshot as one worker_config_log
// row, inside the transaction that changed the worker row.
func (d *WorkerDatastore) appendWorkerConfigLog(ctx context.Context, q datastore.Querier, workerId int64, declaredBy string) error {
	sql := fmt.Sprintf(`
		-- vulkan: worker.appendWorkerConfigLog
		INSERT INTO %[1]s.worker_config_log (worker_id, name, metadata, target_instances, declared_by)
		SELECT
			id,
			name,
			metadata,
			target_instances,
			$2
		FROM %[1]s.worker_config
		WHERE id = $1;
	`, d.Datastore.Schema)
	_, err := q.Exec(ctx, sql, workerId, declaredBy)
	return err
}

// ListWorkers lists the worker rows owned anywhere on owner's chain; a
// system owner also reaches every row below it.
func (d *WorkerDatastore) ListWorkers(ctx context.Context, owner *common.Owner) ([]ListWorkersRow, error) {
	var workers []ListWorkersRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		workers, err = d.listWorkers(ctx, owner)
		return err
	})
	return workers, err
}

func (d *WorkerDatastore) listWorkers(ctx context.Context, owner *common.Owner) ([]ListWorkersRow, error) {
	// one clause per level of the owner chain
	// or all workers if owner is system.
	sql := fmt.Sprintf(`
		-- vulkan: worker.listWorkers
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
		FROM %[1]s.worker_config w
		LEFT JOIN %[1]s.consumer_group_config g ON g.id = w.consumer_group_id
		LEFT JOIN %[1]s.topic_config t ON t.id = COALESCE(w.topic_id, g.topic_id)
		WHERE w.system_id = $1
			OR w.topic_id = $2
			OR w.consumer_group_id = $3
			-- if owner is system we want every worker
			OR ($2 = 0 AND $3 = 0 AND t.system_id = $1);
	`, d.Datastore.Schema)
	rows, err := d.Datastore.Pool.Query(ctx, sql, owner.SystemId, owner.TopicId, owner.ConsumerGroupId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []ListWorkersRow
	for rows.Next() {
		var data ListWorkersRow
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
func (d *WorkerDatastore) GetWorker(ctx context.Context, name string, owner *common.Owner) (*WorkerConfigRow, error) {
	var workerConfigRow *WorkerConfigRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		workerConfigRow, err = d.getWorker(ctx, name, owner)
		return err
	})
	return workerConfigRow, err
}

func (d *WorkerDatastore) getWorker(ctx context.Context, name string, owner *common.Owner) (*WorkerConfigRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: worker.getWorker
		SELECT 
			id, 
			system_id, 
			topic_id, 
			consumer_group_id, 
			name, 
			metadata, 
			target_instances
		FROM %[1]s.worker_config
		WHERE name = $1
			AND system_id IS NOT DISTINCT FROM $2
			AND topic_id IS NOT DISTINCT FROM $3
			AND consumer_group_id IS NOT DISTINCT FROM $4;
	`, d.Datastore.Schema)
	var data WorkerConfigRow
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
