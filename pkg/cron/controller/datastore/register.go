package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
)

// RegisterCronJob resolves register.Name to its row, creating it owned by
// owner if it doesn't exist.
func (d *CronJobDatastore) RegisterCronJob(ctx context.Context, owner *common.Owner, register *RegisterCronJobData) (*CronJobData, error) {
	var job *CronJobData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		job, err = d.registerCronJob(ctx, owner, register)
		return err
	})
	return job, err
}

// registerCronJob registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *CronJobDatastore) registerCronJob(ctx context.Context, owner *common.Owner, register *RegisterCronJobData) (*CronJobData, error) {
	found, err := d.getCronJob(ctx, d.Datastore.Pool, register.Name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		if err := d.assertConfigMatches(found, owner, register); err != nil {
			return nil, err
		}
		d.Logger.InfoContext(ctx, "cron job registered (already existed)", "cron_job", found.Name, "cron_job_id", found.Id)
		return found, nil
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// txn-scoped, per-name -- auto-released at commit/rollback
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('cron_job:' || $1));`, register.Name); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.getCronJob(ctx, tx, register.Name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		if err := d.assertConfigMatches(found, owner, register); err != nil {
			return nil, err
		}
		d.Logger.InfoContext(ctx, "cron job registered (already existed)", "cron_job", found.Name, "cron_job_id", found.Id)
		return found, nil
	}

	dbNow, err := d.dbNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	next := register.Schedule.Next(dbNow)
	if next.IsZero() {
		return nil, fmt.Errorf("schedule %q has no scheduled time after %v", register.Schedule, dbNow)
	}

	insertSql := `
		INSERT INTO cron_job (
			system_id,
			topic_id,
			consumer_group_id,
			name,
			schedule,
			concurrency,
			timeout_ns,
			data,
			metadata,
			next_scheduled_time
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, '{}'::jsonb), COALESCE($9, '{}'::jsonb), $10)
		RETURNING
			id,
			COALESCE(system_id, 0),
			COALESCE(topic_id, 0),
			COALESCE(consumer_group_id, 0),
			name,
			schedule,
			concurrency,
			timeout_ns,
			suspended,
			data,
			metadata,
			next_scheduled_time,
			last_scheduled_time;
	`
	job, err := d.scanCronJobData(tx.QueryRow(ctx, insertSql,
		owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(),
		register.Name, register.Schedule.String(), register.Concurrency, register.TimeoutNs,
		register.Data, register.Metadata, next,
	))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	d.Logger.InfoContext(ctx, "cron job registered (created)", "cron_job", job.Name, "cron_job_id", job.Id, "schedule", job.Schedule, "next_scheduled_time", job.NextScheduledTime)
	return job, nil
}

func (d *CronJobDatastore) assertConfigMatches(found *CronJobData, owner *common.Owner, register *RegisterCronJobData) error {
	dataJson, err := marshalJson(register.Data)
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	metadataJson, err := marshalJson(register.Metadata)
	if err != nil {
		return fmt.Errorf("Metadata: %w", err)
	}

	systemId, topicId, consumerGroupId := owner.IdColumns()
	matches := found.SystemId == systemId &&
		found.TopicId == topicId &&
		found.ConsumerGroupId == consumerGroupId &&
		found.Schedule == register.Schedule.String() &&
		found.Concurrency == register.Concurrency &&
		found.TimeoutNs == register.TimeoutNs &&
		jsonEqual(found.Data, dataJson) &&
		jsonEqual(found.Metadata, metadataJson)
	if !matches {
		return fmt.Errorf("%w: %s: existing=%+v got={SystemId:%d TopicId:%d ConsumerGroupId:%d Schedule:%s Concurrency:%s Timeout:%v Data:%s Metadata:%s}",
			cron.ErrCronJobConfigMismatch, found.Name, *found, systemId, topicId, consumerGroupId, register.Schedule, register.Concurrency, time.Duration(register.TimeoutNs), dataJson, metadataJson)
	}
	return nil
}

// marshalJson mirrors what the INSERT stores: nil -> {} (its COALESCE).
func marshalJson(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage(`{}`), nil
	}
	return json.Marshal(v)
}

// jsonEqual matches jsonb's = -- the stored side comes back normalized, so
// key order and whitespace can't count.
func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}
