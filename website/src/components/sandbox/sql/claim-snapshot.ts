// verbatim from pkg/consumergroup/messageconsumer/controller/datastore/fresh_claim.go
// freshClaimMessagesWithCursor -- the template is drift-checked byte-exact; the
// function mirrors the fmt.Sprintf call
import { interpolate } from './interpolate';
import { consumerGroupCursorTable, messageLogTable } from './table-names';

export const claimSnapshotSqlTemplate = `
		-- vulkan: messageconsumer.freshClaimMessagesWithCursor
		SELECT
			(SELECT COALESCE(MAX(id), 0) FROM %[1]s.%[2]s) AS head,
			pg_snapshot_xmax(pg_current_snapshot())::text AS xmax,
			c.claimed,
			c.settled_head,
			c.pending_head
		FROM %[1]s.%[3]s c
		WHERE c.consumer_group_id = $1;
	`;

export function claimSnapshotSql(topicId: number): string {
	return interpolate(
		claimSnapshotSqlTemplate,
		messageLogTable(topicId),
		consumerGroupCursorTable(topicId),
	);
}
