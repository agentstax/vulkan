// verbatim from pkg/consumergroup/controller/datastore/group.go registerGroup --
// the two %s belong to Postgres's own format(), not to a Go fmt.Sprintf, so this
// statement is passed through as written
export const registerGroupLockSql = `
		-- vulkan: consumergroup.registerGroup
		SELECT pg_advisory_xact_lock(hashtext(format('consumer_group:%s:%s', $1::bigint, $2::text)));
	`;
