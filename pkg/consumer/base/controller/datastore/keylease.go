package datastore

import (
	"context"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ClaimKeyLease attempts to acquire the exclusive right to run a keyed
// message. Acquired guarantees the message was still its key's compaction
// head after the lease was won.
// Expiry does not stop a holder: the next claim on the key takes the lease
// over, and the two runs can overlap until the old one returns.
func (d *KeyLeaseDatastore) ClaimKeyLease(ctx context.Context, topicId int64, groupId int64, key string, messageId int64, duration time.Duration) (*KeyLeaseData, error) {
	// generated once, outside the retry loop -- see the token match in claimSql
	token, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	var claim *KeyLeaseData
	err = d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claim, err = d.claimKeyLease(ctx, topicId, groupId, key, messageId, duration, pgtype.UUID{Bytes: token, Valid: true})
		return err
	})
	return claim, err
}

func (d *KeyLeaseDatastore) claimKeyLease(ctx context.Context, topicId int64, groupId int64, key string, messageId int64, duration time.Duration, token pgtype.UUID) (*KeyLeaseData, error) {
	// the head check gates the insert -- a superseded message never creates
	// or locks a lease row.
	claimSql := `
		WITH head AS (
			SELECT head_id
			FROM compaction_head
			WHERE topic_id = $1
				AND compaction_key = $2
		), attempt AS (
			INSERT INTO key_lease (consumer_group_id, compaction_key, lease_token, expires_at)
			SELECT $3, $2, $6, now() + make_interval(secs => $5)
			WHERE EXISTS (SELECT 1 FROM head WHERE head_id = $4)
			ON CONFLICT (consumer_group_id, compaction_key) DO UPDATE
			SET
				lease_token = $6,
				expires_at = now() + make_interval(secs => $5)
			-- the token match lets a retry after an ambiguous commit re-take its
			-- own lease instead of reading it as busy
			WHERE key_lease.expires_at < now() OR key_lease.lease_token = $6
			RETURNING lease_token
		)
		SELECT
			EXISTS (SELECT 1 FROM head WHERE head_id = $4),
			(SELECT lease_token FROM attempt);
	`

	// the claimSql head CTE snapshot could be stale on the INSERT that
	// follows -- this rechecks with a fresh snapshot and deletes the
	// acquisition if the head moved. So a failed batch rolls the insert
	// back instead of orphaning the lease.
	recheckSql := `
		DELETE FROM key_lease
		WHERE consumer_group_id = $3
			AND compaction_key = $2
			AND lease_token = $5
			AND NOT EXISTS (
				SELECT 1
				FROM compaction_head
				WHERE topic_id = $1
					AND compaction_key = $2
					AND head_id = $4
			);
	`

	// one round trip
	batch := &pgx.Batch{}
	batch.Queue(claimSql, topicId, key, groupId, messageId, duration.Seconds(), token)
	batch.Queue(recheckSql, topicId, key, groupId, messageId, token)

	claim := KeyLeaseData{ConsumerGroupId: groupId, CompactionKey: key}
	var isHead bool

	// claimSql
	results := d.Datastore.Pool.SendBatch(ctx, batch)
	if err := results.QueryRow().Scan(&isHead, &claim.Token); err != nil {
		results.Close()
		return nil, err
	}

	// recheckSql
	recheckTag, err := results.Exec()
	if err != nil {
		results.Close()
		return nil, err
	}
	if err := results.Close(); err != nil {
		return nil, err
	}

	switch {
	case !isHead:
		claim.Verdict = KeyLeaseSuperseded
	case !claim.Token.Valid:
		claim.Verdict = KeyLeaseBusy
	case recheckTag.RowsAffected() > 0:
		// the head moved mid-claim -- the newer version must run, not this one
		claim.Verdict = KeyLeaseSuperseded
		claim.Token = pgtype.UUID{}
	default:
		claim.Verdict = KeyLeaseAcquired
	}
	return &claim, nil
}

// ReleaseKeyLease frees an acquired key.
// false -> no row matched the claim's Token: the lease expired, and the
// row was taken over or deleted by the janitor.
func (d *KeyLeaseDatastore) ReleaseKeyLease(ctx context.Context, claim *KeyLeaseData) (bool, error) {
	var released bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		released, err = d.releaseKeyLease(ctx, d.Datastore.Pool, claim)
		return err
	})
	return released, err
}

func (d *KeyLeaseDatastore) releaseKeyLease(ctx context.Context, q datastore.Querier, claim *KeyLeaseData) (bool, error) {
	sql := `
		DELETE FROM key_lease
		WHERE consumer_group_id = $1
			AND compaction_key = $2
			AND lease_token = $3;
	`
	tag, err := q.Exec(ctx, sql, claim.ConsumerGroupId, claim.CompactionKey, claim.Token)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
