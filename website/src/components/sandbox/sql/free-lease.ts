// verbatim from pkg/consume/messageconsumer/controller/datastore/commit.go
// commit -- the template is drift-checked byte-exact; the function mirrors the
// fmt.Sprintf call
import { interpolate } from './interpolate';
import { claimLeaseTable } from './table-names';

export const freeLeaseSqlTemplate = `
		-- vulkan: messageconsumer.commit
		DELETE FROM %[1]s.%[2]s
		WHERE consumer_group_id = $1
			AND token = $2;
	`;

export function freeLeaseSql(topicId: number): string {
	return interpolate(freeLeaseSqlTemplate, claimLeaseTable(topicId));
}
