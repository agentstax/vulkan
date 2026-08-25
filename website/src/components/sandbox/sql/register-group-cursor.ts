// verbatim from pkg/consumergroup/controller/datastore/group.go registerGroup -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from './interpolate';
import { cursorTable } from './table-names';

export const registerGroupCursorSqlTemplate = `
		-- vulkan: consumergroup.registerGroup
		INSERT INTO %s (consumer_group_id)
		VALUES ($1);
	`;

export function registerGroupCursorSql(topicId: number): string {
	return interpolate(registerGroupCursorSqlTemplate, cursorTable(topicId));
}
