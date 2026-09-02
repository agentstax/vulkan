// verbatim from pkg/consumergroup/controller/datastore/group.go getGroup -- the
// statement names no per-topic table, so it needs no interpolation
export const getGroupSql = `
		-- vulkan: consumergroup.getGroup
		SELECT id, topic_id, name, created_at
		FROM %[1]s.consumer_group_config
		WHERE topic_id = $1 AND name = $2;
	`;
