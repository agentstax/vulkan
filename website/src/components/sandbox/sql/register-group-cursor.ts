// verbatim from pkg/consume/controller/datastore/group.go insertCursor -- the
// templates are drift-checked byte-exact; the functions mirror the fmt.Sprintf calls
import { interpolate } from './interpolate';
import { consumerGroupCursorTable, messageLogTable } from './table-names';

export const insertCursorBeginningSqlTemplate = `
			-- vulkan: consume.insertCursor
			INSERT INTO %[1]s.%[2]s (consumer_group_id)
			VALUES ($1)
			RETURNING committed;
		`;

export const insertCursorHeadSqlTemplate = `
			-- vulkan: consume.insertCursor
			INSERT INTO %[1]s.%[2]s (consumer_group_id, claimed, committed, settled_head)
			SELECT $1, head, head, head
			FROM (SELECT COALESCE(MAX(id), 0) AS head FROM %[1]s.%[3]s) AS log
			RETURNING committed;
		`;

export function insertCursorBeginningSql(topicId: number): string {
	return interpolate(insertCursorBeginningSqlTemplate, consumerGroupCursorTable(topicId));
}

export function insertCursorHeadSql(topicId: number): string {
	return interpolate(
		insertCursorHeadSqlTemplate,
		consumerGroupCursorTable(topicId),
		messageLogTable(topicId),
	);
}
