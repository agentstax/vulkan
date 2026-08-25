import type { DiagnoseQuery } from '../../components/diagnose-queries/types';

// The declared diagnose queries, hand-copied while the shape is under review.
// The library's own declarations are the source of truth and this file is not
// -- only VK0029 is filled in, as the one page being reviewed.
const declared: Record<string, DiagnoseQuery[]> = {
	VK0029: [
		{
			label: 'the delivery row the dead-lettering wrote',
			sql: `SELECT
	status,
	attempts,
	last_error,
	updated_at
FROM delivery_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id = {message_id};`,
		},
		{
			label: 'every attempt it made, oldest first',
			sql: `SELECT
	attempt,
	status,
	error,
	attempted_at
FROM delivery_log_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id = {message_id}
ORDER BY attempt;`,
		},
	],
};

// diagnoseQueries returns null for a code that declares none -- most
// conditions have nothing to look at, and no section renders for them.
export function diagnoseQueries(code: string): DiagnoseQuery[] | null {
	return declared[code] ?? null;
}
