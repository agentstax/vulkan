import { caughtMessage } from '../../helpers/caught-message';
import type { Consumer, ConsumerLine, ConsumerStatus } from '../consumer-card/types';
import { AutoRunner } from './auto-run';
import type { ClaimedMessage } from './database';
import { DatabaseState } from './database-state.svelte';

// what a card's status bar reads before its first tick, the sibling of a
// query panel showing 0 rows before its first run
const noTicksYet: ConsumerStatus = { text: 'no runs yet', tone: 'plain' };

// The sandbox's whole state: the one database, the consumer cards, and each
// control's own error and in-flight flags. The .svelte file renders this and
// owns nothing else.
export class SandboxState {
	readonly databaseState = new DatabaseState();

	produceDescription = $state('expedite shipping');
	produceError: string | null = $state(null);
	producing = $state(false);

	// consumer 1 comes with the page so the mechanism has an object to act on;
	// every other card is one the reader added
	consumers: Consumer[] = $state([seededConsumer()]);
	groups: string[] = $state([]);
	addError: string | null = $state(null);
	adding = $state(false);
	resetting = $state(false);

	busy = $derived(this.databaseState.status === 'connecting' || this.resetting);

	// bootFailed stays out of busy on purpose: it holds the produce, add, and
	// consumer controls shut, while Reset stays live as the retry
	bootFailed = $derived(this.databaseState.status === 'failed');

	// never the card count: removing consumer 2 and adding again would reuse the
	// name, and the grid keys its cards by it
	private nextConsumer = 2;

	private autoRunner = new AutoRunner((name) => this.tickConsumer(name));

	// the database starts booting as soon as the sandbox is on screen, so a
	// panel's first run only waits on the statement; a boot failure is
	// reported by the notice over the panels, with each panel's own run
	// carrying the caught detail.
	// The seeded card's clock starts here too -- its first tick lands about a
	// second in, which the boot it awaits has usually beaten.
	connect(): void {
		this.databaseState
			.connect()
			.then(() => {
				this.refreshGroups();
				this.autoRunner.start(this.consumers[0]!.name);
			})
			.catch(() => {});
	}

	// the island goes away on a view transition; its timers and the Postgres
	// behind it would not
	close(): void {
		this.autoRunner.stopAll();
		void this.databaseState.close();
	}

	// a page control, not an API verb: the database is dropped and rebuilt from
	// the seed, and the cards go with it -- their lines describe ticks against a
	// database that no longer exists.
	async reset(): Promise<void> {
		this.resetting = true;

		// every clock stops before the handle is dropped: a tick landing inside
		// the rebuild would claim against a database being closed underneath it
		this.autoRunner.stopAll();

		try {
			await this.databaseState.reset();
			this.consumers = [seededConsumer()];
			this.nextConsumer = 2;
			this.produceError = null;
			this.addError = null;
			this.autoRunner.start(this.consumers[0]!.name);
			await this.refreshGroups();
		} catch {
			// a database that could not be rebuilt shows the boot notice again,
			// and the run each panel makes on the bumped revision has the detail
		} finally {
			this.resetting = false;
		}
	}

	async produce(): Promise<void> {
		this.producing = true;

		try {
			await this.databaseState.produce(this.produceDescription);
			this.produceError = null;
		} catch (caught) {
			this.produceError = caughtMessage(caught);
		} finally {
			this.producing = false;
		}
	}

	// a new group is a write -- registerGroup is what creates the cursor row it
	// replays from. Joining an existing group only adds a card: that group's
	// cursor is already there, and the two consumers claim off it in turn.
	async addConsumer(group: string | null): Promise<void> {
		this.adding = true;

		try {
			const target = group ?? `group ${this.groups.length + 1}`;
			if (group === null) {
				await this.databaseState.registerGroup(target);
				await this.refreshGroups();
			}

			const name = `consumer ${this.nextConsumer}`;
			this.consumers.push({
				name,
				group: target,
				autoRun: true,
				lines: [{ kind: 'note', text: joinNote(group, target) }],
				status: noTicksYet,
			});
			this.autoRunner.start(name);
			this.nextConsumer += 1;
			this.addError = null;
		} catch (caught) {
			this.addError = caughtMessage(caught);
		} finally {
			this.adding = false;
		}
	}

	setAutoRun(name: string, on: boolean): void {
		const consumer = this.consumers.find((candidate) => candidate.name === name);
		if (consumer === undefined) return;

		consumer.autoRun = on;
		if (on) {
			this.autoRunner.start(name);
			return;
		}
		this.autoRunner.stop(name);
	}

	// the group and its cursor outlive the card: a group with no consumers
	// still holds its place in the log
	removeConsumer(name: string): void {
		this.autoRunner.stop(name);
		this.consumers = this.consumers.filter((consumer) => consumer.name !== name);
	}

	// one tick: claim a range off the group's cursor, hand each message inside it
	// to the handler, free the lease. The handler is this page's -- it succeeds
	// on every message and its only work is the line it writes to the card.
	// Nothing calls this but the consumer's own clock.
	private async tickConsumer(name: string): Promise<void> {
		const consumer = this.consumers.find((candidate) => candidate.name === name);
		if (consumer === undefined) return;

		try {
			const handled: ConsumerLine[] = [];
			const claimed = await this.databaseState.tick(consumer.group, (message) => {
				handled.push({ kind: 'handled', text: handledText(message), status: 'ok' });
			});

			if (claimed === null) {
				consumer.status = { text: 'caught up · nothing to claim', tone: 'plain' };
				return;
			}

			consumer.status = {
				text: claimText(claimed.low, claimed.high, handled.length),
				tone: 'plain',
			};
			consumer.lines.unshift(...handled);
		} catch (caught) {
			// a clock that keeps firing against a database that never came up
			// rewrites the same error every second and never gets past it, so
			// the consumer stops itself -- the unpressed toggle says it did
			this.setAutoRun(name, false);
			consumer.status = {
				text: caughtMessage(caught),
				tone: 'error',
			};
		}
	}

	// the groups a consumer can join are whatever the catalog holds -- the
	// seeded one plus every group Add has registered since
	private async refreshGroups(): Promise<void> {
		try {
			this.groups = await this.databaseState.listGroups();
		} catch {
			// a database that never came up already says so over the panels
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

function seededConsumer(): Consumer {
	return {
		name: 'consumer 1',
		group: 'billing',
		autoRun: true,
		lines: [
			{
				kind: 'note',
				text: "billing's cursor is at 0 —\nits first run claims from the\nstart of the log.",
			},
		],
		status: noTicksYet,
	};
}

function joinNote(group: string | null, target: string): string {
	if (group === null) {
		return `its own cursor, still at 0 —\nits first run reads everything\nproduced so far.`;
	}
	return `shares ${target}'s cursor —\nits runs claim ranges the\nothers have not.`;
}

// the range is the cursor's, the count is what came back inside it: a keyed
// message a newer one on its key replaced is inside the range and unread
function claimText(low: number, high: number, count: number): string {
	return `claim (${low}, ${high}] · ${count} ${count === 1 ? 'message' : 'messages'}`;
}

function handledText(message: ClaimedMessage): string {
	const payload = message.payload;
	return `#${message.id} ${JSON.stringify(payload)}`;
}
