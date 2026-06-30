package waterline

import (
	"context"
	"fmt"
)

// ErrLanesExist means InitLanes was asked to (re)shard a group that already has
// cursor rows. Re-sharding a live group is destructive — it moves block
// boundaries while old leases/deliveries carry the old lane numbers, and resets
// committed/claimed backward — so it is refused. Reset the group's cursors,
// leases, and deliveries first, then InitLanes on the clean group.
var ErrLanesExist = fmt.Errorf("waterline: group already has lanes; reset it before re-sharding")

// ErrLogTooSmall means InitLanes was asked to freeze K lanes over a log with
// fewer than K events, which would assign some lanes an empty frozen block
// (floor==block_hi==0) that can never claim and would pin Watermark/CaughtUp
// forever. Append at least K events before sharding (or use fewer lanes).
var ErrLogTooSmall = fmt.Errorf("waterline: log has fewer events than lanes; append more or use fewer lanes")

// InitLanes is the single-hot-group escape hatch (benchmark: 136k -> 521k at
// K=16). It assigns K disjoint, CONTIGUOUS blocks of the log to lanes 0..K-1
// ONCE from a FROZEN head H, seeding each lane at its block floor with a frozen
// block_hi cap (R1/R4). Because the blocks are frozen and contiguous, lanes
// never overlap (no dup) and never leave a seam (no gap), each lane's committed
// is a true dense frontier within its block, and Watermark = min(committed) is a
// sound group guarantee once every lane has drained into its block.
//
// Use this only for a group that is provably frontier-bound. For a GROWING log,
// prefer more groups over more lanes — striping a growing dense cursor is unsafe
// (see the design doc: the offset%K streaming mode was removed). InitLanes
// freezes the head at call time; offsets appended later are NOT covered by any
// lane and need a fresh (re)assignment or a single catch-up lane.
func (l *PgLog) InitLanes(ctx context.Context, group string, k int) (head int64, err error) {
	if k < 1 {
		k = 1
	}
	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Refuse to re-shard a live group (R5 [2]): re-seeding would reset committed/
	// claimed backward and orphan old leases/deliveries under new lane boundaries.
	var existing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM cursors WHERE consumer_group=$1`, group).Scan(&existing); err != nil {
		return 0, err
	}
	if existing > 0 {
		return 0, ErrLanesExist
	}

	if err := tx.QueryRow(ctx, `SELECT COALESCE(max("offset"),0) FROM events`).Scan(&head); err != nil {
		return 0, err
	}
	// Reject a degenerate freeze (R1/R4 [1]): with head < k some lane would get a
	// frozen empty block (floor==block_hi==0) that never claims and pins the group.
	if head < int64(k) {
		return head, ErrLogTooSmall
	}
	// One row per lane: floor = H*s/k, block_hi = H*(s+1)/k (last lane caps at H).
	const seed = `
		INSERT INTO cursors(consumer_group, lane, committed, claimed, block_hi)
		SELECT $1, s, ($2 * s / $3), ($2 * s / $3),
		       CASE WHEN s = $3 - 1 THEN $2 ELSE $2 * (s + 1) / $3 END
		FROM generate_series(0, $3 - 1) s;`
	if _, err := tx.Exec(ctx, seed, group, head, k); err != nil {
		return 0, err
	}
	return head, tx.Commit(ctx)
}

// CaughtUp reports whether a group has fully drained: its Watermark has reached
// the log head AND no ready|inflight deliveries remain. Uses the contiguous
// waterline (PgLog.Watermark), not min(committed), so it works for sharded groups.
func (l *PgLog) CaughtUp(ctx context.Context, group string) (bool, error) {
	wm, err := l.Watermark(ctx, group)
	if err != nil {
		return false, err
	}
	const q = `
		SELECT $2 >= (SELECT COALESCE(max("offset"),0) FROM events)
		  AND NOT EXISTS (SELECT 1 FROM deliveries
		                   WHERE consumer_group=$1 AND state IN ('ready','inflight'));`
	var done bool
	err = l.Pool.QueryRow(ctx, q, group, wm).Scan(&done)
	return done, err
}
