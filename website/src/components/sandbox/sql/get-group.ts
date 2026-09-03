// verbatim from pkg/consume/controller/datastore/group.go getGroup
import { interpolate } from './interpolate';

export const getGroupSqlTemplate = `
		-- vulkan: consume.getGroup
		SELECT id, topic_id, name, created_at
		FROM %[1]s.consumer_group_config
		WHERE topic_id = $1 AND name = $2;
	`;

export function getGroupSql(): string {
	return interpolate(getGroupSqlTemplate);
}
