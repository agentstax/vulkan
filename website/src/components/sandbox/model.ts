// The sandbox's data shapes, the sibling of a datastore's model.go: the row
// types its queries scan into, and the read-models its verbs hand back.

// table-exact rows, one per query in database.ts
export type GroupRow = { id: number; topic_id: number; name: string; created_at: Date };
export type GroupNameRow = { name: string };
export type ProducedRow = { id: number };

export type SnapshotRow = {
	head: number;
	xmax: string;
	claimed: number;
	settled_head: number;
	pending_head: number;
};

export type CursorRow = { low: number; high: number };

export type LeaseRow = {
	token: string;
	consumer_group_id: number;
	low: number;
	high: number;
	until: Date;
	reclaims: number;
};

export type MessageRow = {
	id: number;
	payload: unknown;
	created_at: Date;
	routing_key: string;
	compaction_key: string;
	compaction_rank: number;
	options: unknown;
};

// what every message on the sandbox topic carries, the seed's and the reader's
// alike. Postgres hands it back with the keys sorted by length -- jsonb has no
// order of its own -- so a caller that prints it names the two fields.
export type OrderPayload = { order_id: number; desc: string };

// one message the claim's range made readable -- the keyed rows a newer message
// on their compaction key replaced never reach this. The payload is jsonb, so
// what comes back is whatever was produced: the caller narrows it.
export type ClaimedMessage = { id: number; payload: unknown };

// the range claimed, the lease held over it, and the rows inside it. Committing
// gives the token back, which is why it travels with the range.
export type ClaimedRange = {
	groupId: number;
	token: string;
	low: number;
	high: number;
	messages: ClaimedMessage[];
};

// one value as the result table prints it. null is SQL NULL, which the table
// renders as its own thing rather than as an empty string
export type ResultCell = string | null;

// one row, its cells in the order result.columns names them
export type ResultRow = ResultCell[];

export type RunResult = {
	columns: string[];
	rows: ResultRow[];
	affectedRows: number | null;
	durationMs: number | null;
	statementCount: number;
};
