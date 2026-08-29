// verbatim from pkg/consumergroup/messageconsumer/controller/datastore/freshclaim.go
// claimMessages -- the template is drift-checked byte-exact; the function mirrors
// the fmt.Sprintf call
import { interpolate } from './interpolate';
import { claimLeaseTable } from './table-names';

export const claimLeaseSqlTemplate = `
		-- vulkan: messageconsumer.claimMessages
		INSERT INTO %s (consumer_group_id, low, high, expires_at)
		VALUES (
			$1,
			$2,
			$3,
			now() + make_interval(secs => $4)
		)
		RETURNING
			token,
			consumer_group_id,
			low,
			high,
			expires_at,
			reclaims;
	`;

export function claimLeaseSql(topicId: number): string {
	return interpolate(claimLeaseSqlTemplate, claimLeaseTable(topicId));
}
