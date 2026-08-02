package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5"
)

type CronJobDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

func NewCronJobDatastore(ds *datastore.PostgresDatastore, retryPolicy *retry.Policy, log logger.Logger) (*CronJobDatastore, error) {
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}

	dsRetry, err := retry.NewDatastoreRetry(retryPolicy, log)
	if err != nil {
		return nil, err
	}

	return &CronJobDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    log,
	}, nil
}

// RegisterCronJob resolves name to its job, creating it if it doesn't exist.
func (d *CronJobDatastore) RegisterCronJob(ctx context.Context, name string, schedule *Schedule, data any, cfg Config) (*CronJob, error) {
	var job *CronJob
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		job, err = d.registerCronJob(ctx, name, schedule, data, cfg)
		return err
	})
	return job, err
}

// registerCronJob registers behind a per-name advisory lock, NOT ON CONFLICT.
// This is to prevent race condition errors between two concurrent calls.
func (d *CronJobDatastore) registerCronJob(ctx context.Context, name string, schedule *Schedule, data any, cfg Config) (*CronJob, error) {
	if !slugPattern.MatchString(name) {
		return nil, fmt.Errorf("name must match %s, got %q", slugPattern, name)
	}
	if schedule == nil {
		return nil, errors.New("schedule is required")
	}
	if cfg.Timeout > schedule.MinRate() {
		return nil, fmt.Errorf("timeout %v exceeds schedule %q's min firing rate %v", cfg.Timeout, schedule, schedule.MinRate())
	}

	// private getCronJob, not GetCronJob -- otherwise would have nested retries.
	found, err := d.getCronJob(ctx, d.Datastore.Pool, name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		if err := d.assertConfigMatches(found, schedule, data, cfg); err != nil {
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
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('cron_job:' || $1));`, name); err != nil {
		return nil, err
	}

	// re-check under the lock -- a racing register may have committed while we waited
	found, err = d.getCronJob(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	if found != nil {
		if err := d.assertConfigMatches(found, schedule, data, cfg); err != nil {
			return nil, err
		}
		d.Logger.InfoContext(ctx, "cron job registered (already existed)", "cron_job", found.Name, "cron_job_id", found.Id)
		return found, nil
	}

	dbNow, err := d.dbNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	next := schedule.Next(dbNow)
	if next.IsZero() {
		return nil, fmt.Errorf("schedule %q never fires after %v", schedule, dbNow)
	}

	insertSql := `
		INSERT INTO cron_job (name, schedule, concurrency, timeout_ns, data, metadata, next_scheduled_time)
		VALUES ($1, $2, $3, $4, COALESCE($5, '{}'::jsonb), COALESCE($6, '{}'::jsonb), $7)
		RETURNING id, data, metadata, next_scheduled_time;
	`
	var id int64
	var dataJson, metadataJson json.RawMessage
	err = tx.QueryRow(ctx, insertSql,
		name, schedule.String(), string(cfg.Concurrency), int64(cfg.Timeout), data, cfg.Metadata, next,
	).Scan(&id, &dataJson, &metadataJson, &next)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	job, err := NewCronJob(id, 0, 0, 0, name, schedule.String(), cfg.Concurrency, cfg.Timeout, false, dataJson, metadataJson, next, nil)
	if err != nil {
		return nil, err
	}
	d.Logger.InfoContext(ctx, "cron job registered (created)", "cron_job", name, "cron_job_id", id, "schedule", schedule, "next_scheduled_time", next)
	return job, nil
}

func (d *CronJobDatastore) assertConfigMatches(found *CronJob, schedule *Schedule, data any, cfg Config) error {
	dataJson, err := marshalJson(data)
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	metadataJson, err := marshalJson(cfg.Metadata)
	if err != nil {
		return fmt.Errorf("Metadata: %w", err)
	}

	matches := found.Schedule == schedule.String() &&
		found.Concurrency == cfg.Concurrency &&
		found.Timeout == cfg.Timeout &&
		jsonEqual(found.Data, dataJson) &&
		jsonEqual(found.Metadata, metadataJson)
	if !matches {
		return fmt.Errorf("%w: %s: existing=%+v got={Schedule:%s Concurrency:%s Timeout:%v Data:%s Metadata:%s}",
			ErrCronJobConfigMismatch, found.Name, *found, schedule, cfg.Concurrency, cfg.Timeout, dataJson, metadataJson)
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

// GetCronJob returns (nil, nil) if not found.
func (d *CronJobDatastore) GetCronJob(ctx context.Context, name string) (*CronJob, error) {
	var job *CronJob
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		job, err = d.getCronJob(ctx, d.Datastore.Pool, name)
		return err
	})
	return job, err
}

func (d *CronJobDatastore) getCronJob(ctx context.Context, q datastore.Querier, name string) (*CronJob, error) {
	sql := `
		SELECT
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
			last_scheduled_time
		FROM cron_job
		WHERE name = $1;
	`
	return d.scanCronJob(q.QueryRow(ctx, sql, name))
}

// UpdateCronJob applies cfg's non-nil fields to the named job.
// Returns (nil, nil) if name is not found.
func (d *CronJobDatastore) UpdateCronJob(ctx context.Context, name string, cfg *AlterConfig) (*CronJob, error) {
	var job *CronJob
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		job, err = d.updateCronJob(ctx, name, cfg)
		return err
	})
	return job, err
}

func (d *CronJobDatastore) updateCronJob(ctx context.Context, name string, cfg *AlterConfig) (*CronJob, error) {
	old, err := d.getCronJob(ctx, d.Datastore.Pool, name)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, nil
	}

	// the effective schedule/timeout pair still has to fit
	sched := cfg.Schedule
	if sched == nil {
		if sched, err = ParseSchedule(old.Schedule); err != nil {
			return nil, fmt.Errorf("schedule %q: %w", old.Schedule, err)
		}
	}
	timeout := old.Timeout
	if cfg.Timeout != nil {
		timeout = *cfg.Timeout
	}
	if timeout > sched.MinRate() {
		return nil, fmt.Errorf("timeout %v exceeds schedule %q's min firing rate %v", timeout, sched, sched.MinRate())
	}

	var next *time.Time
	if cfg.Schedule != nil {
		dbNow, err := d.dbNow(ctx, d.Datastore.Pool)
		if err != nil {
			return nil, err
		}
		n := cfg.Schedule.Next(dbNow)
		if n.IsZero() {
			return nil, fmt.Errorf("schedule %q never fires after %v", cfg.Schedule, dbNow)
		}
		next = &n
	}

	// a nil param reaches Postgres as NULL
	// COALESCE keeps the column's current value if nil passed
	sql := `
		UPDATE cron_job
		SET
			schedule = COALESCE($2, schedule),
			concurrency = COALESCE(NULLIF($3, ''), concurrency),
			timeout_ns = COALESCE($4, timeout_ns),
			data = COALESCE($5, data),
			metadata = COALESCE($6, metadata),
			next_scheduled_time = COALESCE($7, next_scheduled_time)
		WHERE name = $1
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
	row := d.Datastore.Pool.QueryRow(ctx, sql,
		name, scheduleExpr(cfg.Schedule), string(cfg.Concurrency), durationNs(cfg.Timeout), cfg.Data, cfg.Metadata, next,
	)
	updated, err := d.scanCronJob(row)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		// destroyed between the read and the update
		return nil, nil
	}

	d.Logger.InfoContext(ctx, "cron job altered", alterLogFields(old, updated)...)
	return updated, nil
}

// scheduleExpr widens *Schedule to the *string the schedule column stores,
// passing nil through so COALESCE sees NULL.
func scheduleExpr(s *Schedule) *string {
	if s == nil {
		return nil
	}
	expr := s.String()
	return &expr
}

// durationNs widens *time.Duration to the *int64 the _ns columns store,
// passing nil through so COALESCE sees NULL.
func durationNs(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	ns := int64(*d)
	return &ns
}

// alterLogFields renders old -> new pairs for just the fields that changed.
func alterLogFields(old, updated *CronJob) []any {
	fields := []any{"cron_job", updated.Name, "cron_job_id", updated.Id}

	if old.Schedule != updated.Schedule {
		fields = append(fields, "schedule", fmt.Sprintf("%s -> %s", old.Schedule, updated.Schedule))
	}
	if old.Concurrency != updated.Concurrency {
		fields = append(fields, "concurrency", fmt.Sprintf("%s -> %s", old.Concurrency, updated.Concurrency))
	}
	if old.Timeout != updated.Timeout {
		fields = append(fields, "timeout", fmt.Sprintf("%v -> %v", old.Timeout, updated.Timeout))
	}
	if !bytes.Equal(old.Data, updated.Data) {
		fields = append(fields, "data", fmt.Sprintf("%s -> %s", old.Data, updated.Data))
	}
	if !bytes.Equal(old.Metadata, updated.Metadata) {
		fields = append(fields, "metadata", fmt.Sprintf("%s -> %s", old.Metadata, updated.Metadata))
	}
	if !old.NextScheduledTime.Equal(updated.NextScheduledTime) {
		fields = append(fields, "next_scheduled_time", fmt.Sprintf("%v -> %v", old.NextScheduledTime, updated.NextScheduledTime))
	}
	return fields
}

func (d *CronJobDatastore) ListCronJobs(ctx context.Context) ([]*CronJob, error) {
	var jobs []*CronJob
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		jobs, err = d.listCronJobs(ctx)
		return err
	})
	return jobs, err
}

func (d *CronJobDatastore) listCronJobs(ctx context.Context) ([]*CronJob, error) {
	sql := `
		SELECT
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
			last_scheduled_time
		FROM cron_job
		ORDER BY name;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*CronJob
	for rows.Next() {
		job, err := d.scanCronJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (d *CronJobDatastore) SuspendCronJob(ctx context.Context, name string) error {
	return d.Retry.Wrap(ctx, func() error {
		tag, err := d.Datastore.Pool.Exec(ctx, `UPDATE cron_job SET suspended = true WHERE name = $1;`, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s", ErrCronJobNotFound, name)
		}
		d.Logger.InfoContext(ctx, "cron job suspended", "cron_job", name)
		return nil
	})
}

// UnsuspendCronJob resumes at Next(now()) -- a firing that came due while
// suspended is dropped, not fired late.
func (d *CronJobDatastore) UnsuspendCronJob(ctx context.Context, name string) error {
	return d.Retry.Wrap(ctx, func() error {
		return d.unsuspendCronJob(ctx, name)
	})
}

func (d *CronJobDatastore) unsuspendCronJob(ctx context.Context, name string) error {
	job, err := d.getCronJob(ctx, d.Datastore.Pool, name)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("%w: %s", ErrCronJobNotFound, name)
	}

	sched, err := ParseSchedule(job.Schedule)
	if err != nil {
		return fmt.Errorf("schedule %q: %w", job.Schedule, err)
	}
	dbNow, err := d.dbNow(ctx, d.Datastore.Pool)
	if err != nil {
		return err
	}
	next := sched.Next(dbNow)
	if next.IsZero() {
		return fmt.Errorf("schedule %q never fires after %v -- cron job %q stays suspended", job.Schedule, dbNow, name)
	}

	tag, err := d.Datastore.Pool.Exec(ctx, `UPDATE cron_job SET suspended = false, next_scheduled_time = $2 WHERE name = $1;`, name, next)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrCronJobNotFound, name)
	}
	d.Logger.InfoContext(ctx, "cron job unsuspended", "cron_job", name, "next_scheduled_time", next)
	return nil
}

func (d *CronJobDatastore) DestroyCronJob(ctx context.Context, name string) error {
	return d.Retry.Wrap(ctx, func() error {
		tag, err := d.Datastore.Pool.Exec(ctx, `DELETE FROM cron_job WHERE name = $1;`, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s", ErrCronJobNotFound, name)
		}
		d.Logger.WarnContext(ctx, "cron job destroyed", "cron_job", name)
		return nil
	})
}

// with N replicas running in different timezones they all have different
// clocks, getting time from db normalizes to a single source.
func (d *CronJobDatastore) dbNow(ctx context.Context, q datastore.Querier) (time.Time, error) {
	var now time.Time
	err := q.QueryRow(ctx, `SELECT now();`).Scan(&now)
	return now, err
}

// scanCronJob scans a row shaped like getCronJob's SELECT.
func (d *CronJobDatastore) scanCronJob(row pgx.Row) (*CronJob, error) {
	var (
		id                                 int64
		systemId, topicId, consumerGroupId int64
		name, schedule, concurrency        string
		timeoutNs                          int64
		suspended                          bool
		data, metadata                     json.RawMessage
		nextScheduledTime                  time.Time
		lastScheduledTime                  *time.Time
	)
	err := row.Scan(&id, &systemId, &topicId, &consumerGroupId, &name, &schedule, &concurrency,
		&timeoutNs, &suspended, &data, &metadata, &nextScheduledTime, &lastScheduledTime)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return NewCronJob(id, systemId, topicId, consumerGroupId, name, schedule,
		common.ConcurrencyPolicy(concurrency), time.Duration(timeoutNs), suspended, data, metadata,
		nextScheduledTime, lastScheduledTime)
}
