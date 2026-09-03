// verbatim from pkg/consume/controller/datastore/group.go registerGroup.
// The key is a bigint the library derives in Go (common.NewAdvisoryLockKey:
// vulkan's namespace, then a checksum of the schema and the group's name), so
// two installations in one database never wait on each other. The sandbox does
// not model that derivation any more than it models retry policies -- one
// PGlite backend never contends the lock, so any key behaves the same.
export const registerGroupLockSql = `
		-- vulkan: consume.registerGroup
		SELECT pg_advisory_xact_lock($1);
	`;
