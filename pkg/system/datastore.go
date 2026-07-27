package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SystemDatastore owns the shared control-plane schema.
// Tables:
// - cursor
// - lease
// - maintenance
// - binding
// - topic
// - compaction_head
// - migration_log
type SystemDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

func NewSystemDatastore(ds *datastore.PostgresDatastore, retryPolicy *retry.Policy, log logger.Logger) (*SystemDatastore, error) {
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}

	dsRetry, err := retry.NewDatastoreRetry(retryPolicy, log)
	if err != nil {
		return nil, err
	}

	return &SystemDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    log,
	}, nil
}

func (d *SystemDatastore) RegisterSystem(ctx context.Context, cfg Config) error {
	return d.Retry.Wrap(ctx, func() error {
		return d.registerSystem(ctx, cfg)
	})
}

// IsRegistered reports whether RegisterSystem has run -- a system success row in
// migration_log. A missing migration_log table (42P01) counts as not registered.
func (d *SystemDatastore) IsRegistered(ctx context.Context) (bool, error) {
	var registered bool
	err := d.Datastore.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM migration_log
			WHERE entity_type = 'system'
				AND entity_id = 0 
				AND status = 'success'
		);`,
	).Scan(&registered)
	if err != nil {
		// 42P01 = table does not exist
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return false, nil
		}
		return false, err
	}
	return registered, nil
}

// registerSystem creates the shared control-plane schema. Every statement is
// CREATE IF NOT EXISTS -- a no-op against a database that already has the
// tables, a full bootstrap against a fresh one.
//
// This is the BASELINE, after v1 shipped changes to system should be done
// via migration steps.
func (d *SystemDatastore) registerSystem(ctx context.Context, cfg Config) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// txn-scoped -- acquired here, auto-released at commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1);`, common.AdvisoryLock); err != nil {
		return err
	}

	// consumer group cursors for tracking offset in message_log
	createCursorSql := `
		CREATE TABLE IF NOT EXISTS cursor (
			consumer_group TEXT NOT NULL,
			topic_id BIGINT NOT NULL,               -- a group tracks an independent cursor per topic
			claimed BIGINT NOT NULL DEFAULT 0,      -- the read frontier 'inflight' work
			committed BIGINT NOT NULL DEFAULT 0,    -- every message id > committed is in an end state done / dead
			-- the snapshot fence: claims stop at settled_head, not the raw MAX(id),
			-- MAX(id) can sit above uncommitted lower ids -- see FreshClaimMessagesWithCursor
			settled_head BIGINT NOT NULL DEFAULT 0, -- highest id proven to have nothing uncommitted at or below it
			pending_head BIGINT NOT NULL DEFAULT 0, -- candidate head awaiting that proof
			pending_xmax XID8,                      -- txid fence read in the same snapshot as pending_head
			PRIMARY KEY (consumer_group, topic_id)
		);
	`
	if _, err := tx.Exec(ctx, createCursorSql); err != nil {
		return err
	}

	createLeaseSql := `
		CREATE TABLE IF NOT EXISTS lease (
			token UUID NOT NULL DEFAULT gen_random_uuid(),
			consumer_group TEXT NOT NULL,
			topic_id BIGINT NOT NULL,        -- this is for range interpretation (which message_log_<id>)
			low BIGINT NOT NULL,             -- low of claimed range of lease
			high BIGINT NOT NULL,            -- high of claimed range of lease
			until TIMESTAMPTZ NOT NULL,      -- when the lease is considered expired and should be reclaimed
			reclaims INT NOT NULL DEFAULT 0, -- times this range has been reclaimed; past MaxReclaims it's quarantined
			PRIMARY KEY (token, consumer_group)
		);
	`
	if _, err := tx.Exec(ctx, createLeaseSql); err != nil {
		return err
	}

	// maintenance duties: one row per claimable background job. N processes
	// race a conditional UPDATE on can_run_after each tick; the winner runs
	// the duty, losers match zero rows -- one effective worker per interval
	// with no leader election. Also the fleet daemon's discovery index:
	// "what duties exist" and "whose turn" are the same query.
	createMaintenanceSql := `
		CREATE TABLE IF NOT EXISTS maintenance (
			duty TEXT NOT NULL,                               -- 'janitor' | 'waterline'
			topic_id BIGINT NOT NULL,
			consumer_group TEXT NOT NULL DEFAULT '',          -- '' for topic-scoped duties (janitor)
			token UUID NOT NULL DEFAULT gen_random_uuid(),    -- rotates on every claim; renew/release fence on it so only the current owner can touch the claim
			can_run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			attempts INT NOT NULL DEFAULT 0,                  -- incremented on every claim. resets on success
			PRIMARY KEY (duty, topic_id, consumer_group)
		);
	`
	if _, err := tx.Exec(ctx, createMaintenanceSql); err != nil {
		return err
	}

	// bindings: routing rules. A group with no binding matches all events; a
	// group WITH a binding only receives events whose routing_key matches
	// `pattern`.
	createBindingSql := `
		CREATE TABLE IF NOT EXISTS binding (
			id BIGSERIAL PRIMARY KEY,
			consumer_group TEXT NOT NULL,
			topic_id BIGINT NOT NULL,
			pattern TEXT NOT NULL,   -- POSIX regex translated from the NATS-style pattern
			display TEXT             -- original NATS pattern, for humans
		);
	`
	if _, err := tx.Exec(ctx, createBindingSql); err != nil {
		return err
	}
	createBindingIndexSql := `CREATE INDEX IF NOT EXISTS binding_group ON binding (consumer_group, topic_id);`
	if _, err := tx.Exec(ctx, createBindingIndexSql); err != nil {
		return err
	}

	createTopicSql := `
		CREATE TABLE IF NOT EXISTS topic (
			id BIGSERIAL PRIMARY KEY,                                      -- corresponding id for table interpolation ie message_log_<id>
			name TEXT NOT NULL,                                            -- user defined and displayed name
			schema_version BIGINT NOT NULL,                                -- a version bump is a whole new topic row; unrelated to migration_log.migration_version below (the DB-migration axis)
			partition_size BIGINT NOT NULL,                                -- immutable after creation; message_log_<id>'s partition boundaries depend on it staying fixed
			retention_ttl_ns BIGINT NOT NULL DEFAULT 0,                    -- nanoseconds, time.Duration's own unit; 0 disables retention
			allow_drop_past_committed BOOLEAN NOT NULL DEFAULT false,      -- opt into Kafka's "lagging consumer falls off the retention window" semantics
			idempotency_key_ttl_ns BIGINT NOT NULL DEFAULT 86400000000000, -- nanoseconds; unlike retention_ttl_ns, 0 isn't a supported "keep forever" value -- Config.SetDefaults never lets it reach 0, so the column default is the real 24h value, not 0
			disable_delivery_log BOOLEAN NOT NULL DEFAULT false,           -- opt out of delivery_log_<id> (per-attempt failure audit trail)
			janitor_poll_rate_ns BIGINT NOT NULL DEFAULT 5000000000,       -- nanoseconds; how often the janitor loop ticks (create-ahead, drop/sweep expired partitions, sweep idempotency_key)
			janitor_sweep_batch_size INT NOT NULL DEFAULT 1000,            -- rows deleted per sweep transaction; caps how much of a backlog one batch holds a lock for
			waterline_poll_rate_ns BIGINT NOT NULL DEFAULT 1000000000,     -- nanoseconds; how often the waterline duty rolls committed forward -- 1s bounds the crash-recovery redelivery window without churning the cursor row
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (name, schema_version)
		);
	`
	if _, err := tx.Exec(ctx, createTopicSql); err != nil {
		return err
	}

	// O(1) index for compaction's "is this the winner for its key" lookup --
	// upserted synchronously in the same transaction as every keyed publish,
	// never a background job. Shared across topics (not per-topic like
	// message_log) since it scales with DISTINCT compaction_key count, not
	// total message volume.
	createCompactionHeadSql := `
		CREATE TABLE IF NOT EXISTS compaction_head (
			topic_id        BIGINT NOT NULL,           -- PK
			compaction_key  TEXT   NOT NULL,           -- PK
			head_id         BIGINT NOT NULL,           -- the winning message_log id for this key
			compaction_rank BIGINT NOT NULL DEFAULT 0, -- the winner's rank
			PRIMARY KEY (topic_id, compaction_key)
		);
	`
	if _, err := tx.Exec(ctx, createCompactionHeadSql); err != nil {
		return err
	}

	// migration_log is the append-only history of migration attempts
	// -- one row per attempt. entity_type + entity_id say whose:
	// ('system', 0) for the control plane, ('topic', topic_id) per topic.
	createMigrationLogSql := `
		CREATE TABLE IF NOT EXISTS migration_log (
			id BIGSERIAL PRIMARY KEY,
			entity_type TEXT NOT NULL,    -- 'system' | 'topic'
			entity_id BIGINT NOT NULL,    -- 0 for system; topic_id for a topic
			migration_version BIGINT NOT NULL,
			status TEXT NOT NULL,         -- 'success' | 'failure' (extensible)
			error TEXT,                   -- populated when status = 'failure'
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	if _, err := tx.Exec(ctx, createMigrationLogSql); err != nil {
		return err
	}

	// system: the singleton config row. id pinned to 0.
	createSystemSql := `
		CREATE TABLE IF NOT EXISTS system (
			id INT PRIMARY KEY DEFAULT 0,
			advisor_poll_rate_ns BIGINT NOT NULL,          -- nanoseconds; how often the advisor duty runs
			advisory_repeat_interval_ns BIGINT NOT NULL,   -- nanoseconds; how long a firing advisory stays quiet before re-emitting
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	if _, err := tx.Exec(ctx, createSystemSql); err != nil {
		return err
	}

	// Record the system v1 baseline, but only if there's no success row yet
	recordBaselineSql := `
		INSERT INTO migration_log (entity_type, entity_id, migration_version, status)
		SELECT 'system', 0, 1, 'success'
		WHERE NOT EXISTS (
			SELECT 1 FROM migration_log
			WHERE entity_type = 'system'
				AND entity_id = 0 
				AND status = 'success'
		);
	`
	if _, err := tx.Exec(ctx, recordBaselineSql); err != nil {
		return err
	}

	// Seed the config row with cfg. ON CONFLICT DO NOTHING -- the first register
	// wins, later ones no-op. A later register whose cfg DIFFERS returns a mismatch error.
	seedSystemSql := `
		INSERT INTO system (id, advisor_poll_rate_ns, advisory_repeat_interval_ns)
		VALUES (0, $1, $2)
		ON CONFLICT (id) DO NOTHING -- no row returned on conflict -> getConfig and compare (below)
		RETURNING id;
	`
	var seededId int
	seedErr := tx.QueryRow(ctx, seedSystemSql, int64(cfg.AdvisorPollRate), int64(cfg.AdvisoryRepeatInterval)).Scan(&seededId)
	switch {
	case seedErr == nil:
		// won the seed -- the row now holds exactly cfg
	case errors.Is(seedErr, pgx.ErrNoRows):
		existing, err := d.getConfig(ctx, tx)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("system config row missing right after seed -- unexpected")
		}
		want := cfg.ToSystem(existing.CreatedAt, existing.UpdatedAt)
		if *existing != *want {
			return fmt.Errorf("%w: existing=%+v got=%+v", ErrSystemConfigMismatch, *existing, *want)
		}
	default:
		return seedErr
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.InfoContext(ctx, "system schema registered")
	return nil
}

// GetConfig returns the singleton system config, or (nil, nil) if the system
// hasn't been registered.
func (d *SystemDatastore) GetConfig(ctx context.Context) (*System, error) {
	var sys *System
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		sys, err = d.getConfig(ctx, d.Datastore.Pool)
		return err
	})
	return sys, err
}

// getConfig reads the singleton system row. Returns (nil, nil) if the row isn't there yet.
func (d *SystemDatastore) getConfig(ctx context.Context, q datastore.Querier) (*System, error) {
	sql := `
		SELECT advisor_poll_rate_ns, advisory_repeat_interval_ns, created_at, updated_at
		FROM system
		WHERE id = 0;
	`
	var advisorPollRateNs, advisoryRepeatIntervalNs int64
	var createdAt, updatedAt time.Time
	err := q.QueryRow(ctx, sql).Scan(&advisorPollRateNs, &advisoryRepeatIntervalNs, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return NewSystem(time.Duration(advisorPollRateNs), time.Duration(advisoryRepeatIntervalNs), createdAt, updatedAt)
}

// UpdateConfig applies cfg's non-nil fields to the singleton system row and
// returns the updated config. Returns (nil, nil) if the row isn't there.
func (d *SystemDatastore) UpdateConfig(ctx context.Context, cfg *AlterConfig) (*System, error) {
	var sys *System
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		sys, err = d.updateConfig(ctx, cfg)
		return err
	})
	return sys, err
}

func (d *SystemDatastore) updateConfig(ctx context.Context, cfg *AlterConfig) (*System, error) {
	// read-before-write is only for the old -> new log line
	old, err := d.getConfig(ctx, d.Datastore.Pool)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, nil
	}

	// a nil param reaches Postgres as NULL; COALESCE keeps the current value.
	sql := `
		UPDATE system
		SET
			advisor_poll_rate_ns = COALESCE($1, advisor_poll_rate_ns),
			advisory_repeat_interval_ns = COALESCE($2, advisory_repeat_interval_ns),
			updated_at = NOW()
		WHERE id = 0
		RETURNING advisor_poll_rate_ns, advisory_repeat_interval_ns, created_at, updated_at;
	`
	var advisorPollRateNs, advisoryRepeatIntervalNs int64
	var createdAt, updatedAt time.Time
	err = d.Datastore.Pool.QueryRow(ctx, sql, durationNs(cfg.AdvisorPollRate), durationNs(cfg.AdvisoryRepeatInterval)).
		Scan(&advisorPollRateNs, &advisoryRepeatIntervalNs, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// destroyed between the read and the update
			return nil, nil
		}
		return nil, err
	}

	updated, err := NewSystem(time.Duration(advisorPollRateNs), time.Duration(advisoryRepeatIntervalNs), createdAt, updatedAt)
	if err != nil {
		return nil, err
	}
	d.Logger.InfoContext(ctx, "system config altered", alterLogFields(old, updated)...)
	return updated, nil
}

// durationNs widens *time.Duration to the *int64 the _ns columns store, passing
// nil through so COALESCE sees NULL.
func durationNs(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	ns := int64(*d)
	return &ns
}

// alterLogFields renders old -> new pairs for just the fields that changed.
func alterLogFields(old, updated *System) []any {
	fields := []any{}
	if old.AdvisorPollRate != updated.AdvisorPollRate {
		fields = append(fields, "advisor_poll_rate", fmt.Sprintf("%v -> %v", old.AdvisorPollRate, updated.AdvisorPollRate))
	}
	if old.AdvisoryRepeatInterval != updated.AdvisoryRepeatInterval {
		fields = append(fields, "advisory_repeat_interval", fmt.Sprintf("%v -> %v", old.AdvisoryRepeatInterval, updated.AdvisoryRepeatInterval))
	}
	return fields
}
