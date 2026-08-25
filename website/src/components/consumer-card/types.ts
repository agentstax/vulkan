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
// autoRun on means the consumer ticks about once a second on its own clock,
// which is the state a card starts in -- a consumer instance that polls is what
// the library runs, and a card that only moves when clicked reads as though a
// claim were a manual verb. Off, the consumer sits where its last tick left it.
//
// lines are NEWEST FIRST, the same end message_log_1 puts its newest row: a
// tick prepends the messages it read, so the card needs no scrolling to show
// what just happened.
export type Consumer = {
	name: string;
	group: string;
	autoRun: boolean;
	lines: ConsumerLine[];
	status: ConsumerStatus;
};
