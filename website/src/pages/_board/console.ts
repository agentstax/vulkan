// Template content for the static console shell. The live PGlite console
// replaces this module: the SQL becomes the editor's starting document and
// the rows come from a real query against the browser database.
export const consoleLabel = 'your queue, selected';

export const consoleSql = `SELECT id, routing_key, payload
FROM message_log_1
ORDER BY id DESC;`;

export const consoleColumns = ['id', 'routing_key', 'payload'];

export const consoleRows: (string | null)[][] = [
	['2', null, '{"order_id": 43, "amount_cents": 250}'],
	['1', 'orders.eu.created', '{"order_id": 42, "amount_cents": 1999}'],
];
