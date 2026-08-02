package maintain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Scheduler runs the system's scheduler duty:
// - scanning cron_job for due rows each tick
// - producing one JobRequest per due firing to __system.job_requests
// - advancing each fired row to its next firing.
// Scoped to (system) -- one effective Scheduler per system.
type Scheduler struct {
	System    *system.System // resolved by Register from the singleton system row
	Datastore *MaintenanceDatastore
	Config    *MaintainerConfig
	Logger    logger.Logger // copied from Config.Logger at construction

	systemDatastore *system.SystemDatastore
	producer        *producer.Producer[cron.JobRequest]
	duty            *dutyRunner // constructed by Register -- identity and rate come from the offered maintenance row
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewScheduler(ds *datastore.PostgresDatastore, cfg *MaintainerConfig) (*Scheduler, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &MaintainerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	maintenanceDatastore, err := NewMaintenanceDatastore(ds, &MaintenanceDatastoreConfig{
		Logger:    cfg.Logger,
		Retry:     cfg.Retry,
		DutyRetry: cfg.DutyRetry,
	})
	if err != nil {
		return nil, err
	}

	systemDatastore, err := system.NewSystemDatastore(ds, cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	jobProducer, err := producer.NewProducer[cron.JobRequest](cron.TopicName, topic.SchemaVersion(1), ds, &producer.ProducerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Scheduler{
		Datastore:       maintenanceDatastore,
		Config:          cfg,
		Logger:          cfg.Logger,
		systemDatastore: systemDatastore,
		producer:        jobProducer,
	}, nil
}

// shouldRegister reports whether this duty runs the passed duty kind.
func (s *Scheduler) shouldRegister(duty string) bool {
	return duty == DutyScheduler
}

// Register resolves the singleton system row and starts the producer's
// lifecycle. (false, nil) declines a row of another kind.
func (s *Scheduler) Register(ctx context.Context, duty string, owner *common.Owner, meta *DutyMetadata) (bool, error) {
	if !s.shouldRegister(duty) {
		return false, nil
	}
	if s.System != nil {
		return false, errors.New("scheduler already registered")
	}
	if owner == nil {
		return false, errors.New("owner must not be nil")
	}
	if meta == nil {
		return false, errors.New("metadata must not be nil")
	}

	sys, err := s.systemDatastore.GetConfig(ctx)
	if err != nil {
		return false, err
	}
	if sys == nil {
		return false, fmt.Errorf("%w -- register the system with MessageAdmin.RegisterSystem first", migrate.ErrNotRegistered)
	}

	if err := migrate.AssertSystemSchemaSupported(ctx, s.Datastore.Datastore.Pool); err != nil {
		return false, err
	}

	if err := s.producer.Register(ctx); err != nil {
		return false, err
	}

	runner, err := newDutyRunner(s.Datastore, s.Logger, s.Config.JitterFraction, DutyScheduler, owner, meta.PollRate)
	if err != nil {
		return false, err
	}

	s.System = sys
	s.duty = runner
	return true, nil
}

// Run ticks the scheduler duty until ctx cancels; a requested stop returns nil.
func (s *Scheduler) Run(ctx context.Context) error {
	if s.System == nil {
		return errors.New("scheduler not registered -- call Register first")
	}

	s.Logger.InfoContext(ctx, "scheduler duty loop starting", "system", s.System.Id)

	err := s.duty.run(ctx, s.tick)
	if errors.Is(err, context.Canceled) {
		s.Logger.InfoContext(ctx, "scheduler stopped", "system", s.System.Id)
		return nil
	}
	return err
}

// tick is one scheduler pass: an unlocked scan for due rows, then ONE
// transaction per row -- a shared transaction would let one bad row roll back
// every job's produce, and would hold ProduceInTx's whole-topic
// consumer-progress lock from the first produce to the end of the pass.
func (s *Scheduler) tick(ctx context.Context) error {
	ids, err := s.Datastore.DueCronJobs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.produceJobRequest(ctx, id); err != nil {
			if ctx.Err() != nil {
				return err
			}
			s.Logger.WarnContext(ctx, "cron job firing failed -- siblings proceed", "cron_job", id, "error", err)
		}
	}
	return nil
}

// DueCronJobs lists the cron jobs due to fire. Unlocked -- each row is
// rechecked under its own lock before it fires.
func (d *MaintenanceDatastore) DueCronJobs(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		sql := `
			SELECT id FROM cron_job
			WHERE next_scheduled_time <= now() AND NOT suspended
			ORDER BY next_scheduled_time;
		`
		rows, err := d.Datastore.Pool.Query(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()

		var scanned []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			scanned = append(scanned, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		ids = scanned
		return nil
	})
	return ids, err
}

// produceJobRequest fires one due row: recheck under lock, produce the NEWEST
// due firing, advance the row. Produce + advance + idempotency claim share the
// transaction, so an ambiguous-commit replay rolls all three back together and
// the FiringKey dedupe covers exactly that replay.
func (s *Scheduler) produceJobRequest(ctx context.Context, id int64) error {
	return producer.InTransaction(ctx, s.Datastore.Datastore, func(ctx context.Context, tx producer.Tx) error {
		row, err := claimDueCronJob(ctx, tx, id)
		if err != nil || row == nil {
			return err
		}

		sched, err := cron.ParseSchedule(row.schedule)
		if err != nil {
			return err
		}

		// fire the NEWEST due firing -- after downtime, staleness is at most
		// one firing rate; older due firings are dropped, not fired late. The
		// IsZero guard keeps an unsatisfiable schedule from spinning.
		firing := row.nextScheduledTime
		for n := sched.Next(firing); !n.IsZero() && !n.After(row.dbNow); n = sched.Next(firing) {
			firing = n
		}

		request, err := cron.NewJobRequest(row.id, row.name, firing, row.data, row.metadata)
		if err != nil {
			return err
		}
		passthrough := func(context.Context, producer.Tx, uuid.UUID) (*cron.JobRequest, error) {
			return request, nil
		}
		_, err = s.producer.ProduceInTx(ctx, tx, passthrough, producer.ProduceOptions{
			RoutingKey:     row.name,
			CompactionKey:  strconv.FormatInt(row.id, 10), // id not name -- a destroyed name's reuse must not share a key
			IdempotencyKey: cron.FiringKey(firing, row.id),
			Message: &common.MessageOptions{
				Concurrency: common.ConcurrencyPolicy(row.concurrency),
				Timeout:     row.timeout,
			},
		})
		if err != nil {
			return err
		}

		// next firing from the DB clock ONLY -- Go/DB skew double-fires tight schedules
		next := sched.Next(row.dbNow)
		if next.IsZero() {
			// schedule went unsatisfiable (tzdata drift): keep the produce,
			// park the row -- next_scheduled_time is NOT NULL and has no honest value
			s.Logger.WarnContext(ctx, "cron job schedule has no next firing -- suspending", "cron_job", row.id, "name", row.name, "schedule", row.schedule)
			_, err = tx.Exec(ctx, `UPDATE cron_job SET suspended = true, last_scheduled_time = $2 WHERE id = $1;`, row.id, firing)
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE cron_job SET next_scheduled_time = $2, last_scheduled_time = $3 WHERE id = $1;`, row.id, next, firing)
		return err
	})
}

// dueCronJob is the locked row snapshot one producing transaction works from.
type dueCronJob struct {
	id                int64
	name              string
	schedule          string
	concurrency       string
	timeout           time.Duration
	data              json.RawMessage
	metadata          json.RawMessage
	nextScheduledTime time.Time
	dbNow             time.Time
}

// claimDueCronJob rereads the row under lock, making the unlocked due scan
// safe -- nil means it raced away (suspended, destroyed, or another
// scheduler's transaction holds it).
func claimDueCronJob(ctx context.Context, tx producer.Tx, id int64) (*dueCronJob, error) {
	sql := `
		SELECT id, name, schedule, concurrency, timeout_ns, data, metadata, next_scheduled_time, now()
		FROM cron_job
		WHERE id = $1 AND next_scheduled_time <= now() AND NOT suspended
		FOR UPDATE SKIP LOCKED;
	`
	var row dueCronJob
	var timeoutNs int64
	err := tx.QueryRow(ctx, sql, id).Scan(&row.id, &row.name, &row.schedule, &row.concurrency,
		&timeoutNs, &row.data, &row.metadata, &row.nextScheduledTime, &row.dbNow)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	row.timeout = time.Duration(timeoutNs)
	return &row, nil
}
