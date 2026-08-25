// verbatim from pkg/consumergroup/messageconsumer/controller/datastore/commit.go
// commit -- the template is drift-checked byte-exact; the function mirrors the
// fmt.Sprintf call
import { interpolate } from './interpolate';
import { leaseTable } from './table-names';

export const freeLeaseSqlTemplate = `
		-- vulkan: messageconsumer.commit
		DELETE FROM %s
		WHERE consumer_group_id = $1
			AND token = $2;
	`;

export function freeLeaseSql(topicId: number): string {
	return interpolate(freeLeaseSqlTemplate, leaseTable(topicId));
}
