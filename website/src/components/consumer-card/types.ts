// One line of a consumer's output.
//
// claim: the id range a tick took off the cursor
// handled: one message the handler was given, and how it came back
// note: standing text about where the consumer sits, with no tick behind it
// error: the tick itself did not finish -- a statement in it returned an error
export type ConsumerLine =
	| { kind: 'claim'; text: string }
	| { kind: 'handled'; text: string; status: 'ok' | 'error' }
	| { kind: 'note'; text: string }
	| { kind: 'error'; text: string };

// A consumer instance and the group whose cursor it claims from. Several
// consumers naming the same group share that one cursor.
//
// lines are NEWEST FIRST, the same end message_log_1 puts its newest row: a
// tick prepends its whole block, so the newest claim is always the top line
// and the card needs no scrolling to show what just happened.
export type Consumer = {
	name: string;
	group: string;
	lines: ConsumerLine[];
};
