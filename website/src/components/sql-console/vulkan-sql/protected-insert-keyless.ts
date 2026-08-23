// verbatim from pkg/producer/controller/datastore/insert.go protectedInsertSQL -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from './interpolate';
import { idempotencyKeyTable, messageLogTable } from './table-names';

export const protectedInsertKeylessSqlTemplate = `
			-- vulkan: producer.protectedInsert
			WITH claim AS (
				INSERT INTO %s (idempotency_key)
				VALUES ($1)
				ON CONFLICT (idempotency_key) DO NOTHING
				RETURNING idempotency_key
			)
			INSERT INTO %s (payload, routing_key, options)
			SELECT
				$2,
				NULLIF($3, ''), -- if routing_key is empty string '' insert as NULL
				$4
			WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
			RETURNING id;
		`;

export function protectedInsertKeylessSql(topicId: number): string {
	return interpolate(protectedInsertKeylessSqlTemplate, idempotencyKeyTable(topicId), messageLogTable(topicId));
}
