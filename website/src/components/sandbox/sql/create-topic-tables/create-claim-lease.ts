// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { claimLeaseTable } from '../table-names';

export const createClaimLeaseSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %[1]s.%[2]s (
			token UUID NOT NULL DEFAULT gen_random_uuid(),
			consumer_group_id BIGINT NOT NULL,
			low BIGINT NOT NULL,             -- low of claimed range of lease
			high BIGINT NOT NULL,            -- high of claimed range of lease
			expires_at TIMESTAMPTZ NOT NULL, -- past it the lease is reclaimed
			reclaims INT NOT NULL DEFAULT 0, -- times this range has been reclaimed; past MaxReclaims it's quarantined
			PRIMARY KEY (token, consumer_group_id)
		);
	`;

export function createClaimLeaseSql(topicId: number): string {
	return interpolate(createClaimLeaseSqlTemplate, claimLeaseTable(topicId));
}
