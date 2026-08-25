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
export type Consumer = {
	name: string;
	group: string;
	lines: ConsumerLine[];
};
