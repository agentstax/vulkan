package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/internal/topic"
)

// committed is the waterline: the mark below which every offset is resolved.
//
// Race Condition:
//
//	Because the claiming transaction in FreshClaimMessagesWithCursor advances
//	cursor.claimed AND inserts a lease we must read both cursor and lease
//	tables back in one SELECT, then write separately. Read them inside a single
//	UPDATE instead and postgres hands back the new claimed row but not the new
//	lease, so committed can advance past a range that is still being processed.
//
//	This is due to READ COMMITTED: an UPDATE re-reads the row it modifies at its
//	newest version, but its subqueries keep the snapshot from when the statement
//	began -- so cursor comes back fresh, lease stale.
func (d *WaterlineDatastore) AdvanceWaterline(ctx context.Context, topicId int64, groupId int64) (int64, error) {
	var committed int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		committed, err = d.advanceWaterline(ctx, topicId, groupId)
		return err
	})
	return committed, err
}

func (d *WaterlineDatastore) advanceWaterline(ctx context.Context, topicId int64, groupId int64) (int64, error) {
	// 1. compute the advance target, LEAST of:
	// 		earliest open lease
	// 		earliest unresolved delivery -- 'dead' be definition does not count
	// 		claimed (its caught up to head of log)
	// LEAST ignores NULLs so any/all of those can be absent.
	targetSql := fmt.Sprintf(`
		SELECT LEAST(
			(SELECT MIN(low) FROM lease WHERE consumer_group_id = $1),
			(SELECT MIN(message_id) - 1 FROM %s WHERE consumer_group_id = $1 AND status IN ('ready', 'inflight', 'deferred')),
			claimed
		)
		FROM cursor
		WHERE consumer_group_id = $1;
	`, topic.DeliveryTable(topicId))

	var target int64
	if err := d.Datastore.Pool.QueryRow(ctx, targetSql, groupId).Scan(&target); err != nil {
		return 0, err
	}

	// 2. apply it. GREATEST -> committed only ever moves forward.
	const rollSql = `
		UPDATE cursor
		SET committed = GREATEST(committed, $2)
		WHERE consumer_group_id = $1
		RETURNING committed;
	`

	var committed int64
	err := d.Datastore.Pool.QueryRow(ctx, rollSql, groupId, target).Scan(&committed)
	return committed, err
}
