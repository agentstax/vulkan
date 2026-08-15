package datastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/jackc/pgx/v5"
)

// InsertWorker creates the (name, owner) worker row.
// Metadata is merged between incoming values and existing overrides.
func (d *WorkerDatastore) InsertWorker(ctx context.Context, name string, owner *common.Owner, metadata any, targetInstances int) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.insertWorker(ctx, name, owner, metadata, targetInstances)
	})
}

func (d *WorkerDatastore) insertWorker(ctx context.Context, name string, owner *common.Owner, metadata any, targetInstances int) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	insertSql := `
		INSERT INTO worker (system_id, topic_id, consumer_group_id, name, metadata, target_instances)
		VALUES ($1, $2, $3, $4, COALESCE($5, '{}'::jsonb), $6)
		ON CONFLICT DO NOTHING;
	`
	if _, err := tx.Exec(ctx, insertSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name, metadata, targetInstances); err != nil {
		return err
	}
	if metadata == nil {
		return tx.Commit(ctx)
	}

	// the row lock serializes against a concurrent alter writing an override
	// between the read and the refresh
	selectSql := `
		SELECT metadata
		FROM worker
		WHERE name = $4
			AND system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3
		FOR UPDATE;
	`
	var existing map[string]any
	if err := tx.QueryRow(ctx, selectSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name).Scan(&existing); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("worker %q was deleted while its declaration was in flight -- likely a concurrent destroy; rerun the declaration if the owner still exists", name)
		}
		return err
	}

	// metadata arrives as the caller's typed struct; the merge needs map form
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	var incoming map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return err
	}

	merged := mergeMetadata(existing, incoming)

	updateSql := `
		UPDATE worker
		SET metadata = $5
		WHERE name = $4
			AND system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3;
	`
	if _, err := tx.Exec(ctx, updateSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name, merged); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AlterWorker applies each overrides key's Update to the (name, owner)
// worker row's metadata and returns the row. Errors if the row was never
// declared or doesn't declare a changed key.
func (d *WorkerDatastore) AlterWorker(ctx context.Context, name string, owner *common.Owner, overrides map[string]common.Update[any]) (*WorkerData, error) {
	var altered *WorkerData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		altered, err = d.alterWorker(ctx, name, owner, overrides)
		return err
	})
	return altered, err
}

func (d *WorkerDatastore) alterWorker(ctx context.Context, name string, owner *common.Owner, overrides map[string]common.Update[any]) (*WorkerData, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 1. the existing metadata, locked against a concurrent declaration
	selectSql := `
		SELECT metadata
		FROM worker
		WHERE name = $4
			AND system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3
		FOR UPDATE;
	`
	var metadata map[string]any
	if err := tx.QueryRow(ctx, selectSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name).Scan(&metadata); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("worker %q has no worker row -- the owner's register declares it", name)
		}
		return nil, err
	}

	// every changed key must exist on the row
	for key, update := range overrides {
		if update.IsChanged() && !declaresKey(metadata, key) {
			return nil, fmt.Errorf("worker %q does not declare metadata key %q", name, key)
		}
	}

	// 2. apply the updates
	applied := applyOverrides(metadata, overrides)

	// 3. write the new metadata
	updateSql := `
		UPDATE worker
		SET metadata = $5
		WHERE name = $4
			AND system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3;
	`
	if _, err := tx.Exec(ctx, updateSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name, applied); err != nil {
		return nil, err
	}

	// 4. return the row as the table now has it
	readSql := `
		SELECT
			id,
			system_id,
			topic_id,
			consumer_group_id,
			name,
			metadata,
			target_instances
		FROM worker
		WHERE name = $4
			AND system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3;
	`
	var data WorkerData
	if err := tx.QueryRow(ctx, readSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), name).
		Scan(&data.Id, &data.SystemId, &data.TopicId, &data.ConsumerGroupId, &data.Name, &data.Metadata, &data.TargetInstances); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &data, nil
}

// AlterWorkers applies each overrides key's Update to every worker row
// owner owns directly, in one transaction, and returns the rows. A changed
// key no row declares fails the whole transaction -- nothing is written.
func (d *WorkerDatastore) AlterWorkers(ctx context.Context, owner *common.Owner, overrides map[string]common.Update[any]) ([]WorkerData, error) {
	var altered []WorkerData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		altered, err = d.alterWorkers(ctx, owner, overrides)
		return err
	})
	return altered, err
}

func (d *WorkerDatastore) alterWorkers(ctx context.Context, owner *common.Owner, overrides map[string]common.Update[any]) ([]WorkerData, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 1. every owned row's metadata, locked against concurrent declarations;
	// name order keeps two concurrent alters locking in the same order
	selectSql := `
		SELECT id, metadata
		FROM worker
		WHERE system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3
		ORDER BY name
		FOR UPDATE;
	`
	rows, err := tx.Query(ctx, selectSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn())
	if err != nil {
		return nil, err
	}
	var workers []WorkerMetadataData
	for rows.Next() {
		var data WorkerMetadataData
		if err := rows.Scan(&data.Id, &data.Metadata); err != nil {
			rows.Close()
			return nil, err
		}
		workers = append(workers, data)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// every changed key must exist on at least one row
	for key, update := range overrides {
		if !update.IsChanged() {
			continue
		}
		declared := false
		for _, data := range workers {
			if declaresKey(data.Metadata, key) {
				declared = true
				break
			}
		}
		if !declared {
			return nil, fmt.Errorf("no worker row of this owner declares metadata key %q", key)
		}
	}

	// 2. apply the updates  3. write each row's new metadata
	updateSql := `UPDATE worker SET metadata = $2 WHERE id = $1;`
	for _, data := range workers {
		applied := applyOverrides(data.Metadata, overrides)
		if _, err := tx.Exec(ctx, updateSql, data.Id, applied); err != nil {
			return nil, err
		}
	}

	// 4. return the rows as the table now has them
	readSql := `
		SELECT
			id,
			system_id,
			topic_id,
			consumer_group_id,
			name,
			metadata,
			target_instances
		FROM worker
		WHERE system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3
		ORDER BY name;
	`
	readRows, err := tx.Query(ctx, readSql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn())
	if err != nil {
		return nil, err
	}
	defer readRows.Close()
	var altered []WorkerData
	for readRows.Next() {
		var data WorkerData
		if err := readRows.Scan(&data.Id, &data.SystemId, &data.TopicId, &data.ConsumerGroupId, &data.Name, &data.Metadata, &data.TargetInstances); err != nil {
			return nil, err
		}
		altered = append(altered, data)
	}
	if err := readRows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return altered, nil
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

// mergeMetadata applies the redeclaration rule to incoming in place:
// incoming defaults win, stored overrides survive, keys absent from
// incoming are dropped.
func mergeMetadata(existing map[string]any, incoming map[string]any) map[string]any {
	for key, incomingValue := range incoming {
		existingLayers, ok := existing[key].(map[string]any)
		if !ok {
			continue
		}
		override, ok := existingLayers["override"]
		if !ok {
			continue
		}
		incomingLayers, ok := incomingValue.(map[string]any)
		if !ok {
			continue
		}
		incomingLayers["override"] = override
	}
	return incoming
}

// declaresKey reports whether metadata carries the key's {default, override}
// layers -- an override exists only on a key the owner's code declares.
func declaresKey(metadata map[string]any, key string) bool {
	_, ok := metadata[key].(map[string]any)
	return ok
}

// applyOverrides returns a copy of metadata with each key's Update applied
// to its {default, override} layers; keys metadata doesn't declare are
// skipped.
func applyOverrides(metadata map[string]any, overrides map[string]common.Update[any]) map[string]any {
	// a fresh copy, layers included -- the caller's metadata stays untouched
	applied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if layers, ok := value.(map[string]any); ok {
			applied[key] = maps.Clone(layers)
			continue
		}
		applied[key] = value
	}

	// set or remove each changed key's override
	for key, update := range overrides {
		layers, ok := applied[key].(map[string]any)
		if !ok {
			continue
		}
		if value, ok := update.Value(); ok {
			layers["override"] = value
		} else if update.IsUnset() {
			delete(layers, "override")
		}
	}
	return applied
}
