// verbatim from pkg/consumergroup/controller/datastore/group.go registerGroup --
// consumer_group is shared catalog schema, so the name carries no topic id
export const registerGroupInsertSql = `
		-- vulkan: consumergroup.registerGroup
		INSERT INTO %[1]s.consumer_group_config (topic_id, name)
		VALUES ($1, $2)
		RETURNING id, topic_id, name, created_at;
	`;
