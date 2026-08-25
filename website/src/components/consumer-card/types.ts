// One line of a consumer's output.
//
// handled: one message the handler was given, and how it came back
// note: standing text about where the consumer sits, with no tick behind it
export type ConsumerLine =
	{ kind: 'handled'; text: string; status: 'ok' | 'error' } | { kind: 'note'; text: string };

// The card's foot: what the LAST tick did, the sibling of a query panel's
// "8 rows · 3.0 ms". The claimed range lives here rather than in the output
// because it describes the tick, not a message. A tick that did not finish
// reports its error here too, under tone 'error'.
export type ConsumerStatus = { text: string; tone: 'plain' | 'error' };

// A consumer instance and the group whose cursor it claims from. Several
// consumers naming the same group share that one cursor.
//
// lines are NEWEST FIRST, the same end message_log_1 puts its newest row: a
// tick prepends the messages it read, so the card needs no scrolling to show
// what just happened.
export type Consumer = {
	name: string;
	group: string;
	lines: ConsumerLine[];
	status: ConsumerStatus;
};
