<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import AddConsumer from '../add-consumer/add-consumer.svelte';
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import DatabaseProgress from '../database-progress/database-progress.svelte';
	import type { Consumer, ConsumerLine, ConsumerStatus } from '../consumer-card/types';
	import ConsumerGrid from '../consumer-grid/consumer-grid.svelte';
	import ProduceMessage from '../produce-message/produce-message.svelte';
	import SqlPanel from '../sql-panel/sql-panel.svelte';
	import { AutoRunner } from './auto-run';
	import type { ClaimedMessage } from './database';
	import { DatabaseState } from './database-state.svelte';
	import type { PanelShell } from './types';

	type Props = {
		label: string;
		topic: string;
		messages: PanelShell;
		cursors: PanelShell;
	};

	let { label, topic, messages, cursors }: Props = $props();

	// what a card's status bar reads before its first tick, the sibling of a
	// query panel showing 0 rows before its first run
	const noTicksYet: ConsumerStatus = { text: 'no runs yet', tone: 'plain' };

	let produceDescription = $state('expedite shipping');
	let produceError: string | null = $state(null);
	let producing = $state(false);

	// consumer 1 comes with the page so the mechanism has an object to act on;
	// every other card is one the reader added
	let consumers: Consumer[] = $state([seededConsumer()]);
	let groups: string[] = $state([]);
	let addError: string | null = $state(null);
	let adding = $state(false);
	let resetting = $state(false);

	// never the card count: removing consumer 2 and adding again would reuse the
	// name, and the grid keys its cards by it
	let nextConsumer = 2;

	const databaseState = new DatabaseState();
	const autoRunner = new AutoRunner(tickConsumer);
	const busy = $derived(databaseState.status === 'connecting' || resetting);

	// the database starts booting as soon as the sandbox is on screen, so a
	// panel's first run only waits on the statement; a boot failure reaches
	// every panel through its own run, which is where it is reported.
	// The seeded card's clock starts here too -- its first tick lands about a
	// second in, which the boot it awaits has usually beaten.
	onMount(() => {
		databaseState
			.connect()
			.then(() => {
				refreshGroups();
				autoRunner.start(consumers[0]!.name);
			})
			.catch(() => {});
	});

	// the island goes away on a view transition; its timers and the Postgres
	// behind it would not
	onDestroy(() => {
		autoRunner.stopAll();
		void databaseState.close();
	});

	// a page control, not an API verb: the database is dropped and rebuilt from
	// the seed, and the cards go with it -- their lines describe ticks against a
	// database that no longer exists.
	async function reset(): Promise<void> {
		resetting = true;

		// every clock stops before the handle is dropped: a tick landing inside
		// the rebuild would claim against a database being closed underneath it
		autoRunner.stopAll();

		try {
			await databaseState.reset();
			consumers = [seededConsumer()];
			nextConsumer = 2;
			produceError = null;
			addError = null;
			autoRunner.start(consumers[0]!.name);
			await refreshGroups();
		} catch {
			// a database that could not be rebuilt reports itself in both panels,
			// through the run each one makes on the revision the reset bumped
		} finally {
			resetting = false;
		}
	}

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

	// the groups a consumer can join are whatever the catalog holds -- the
	// seeded one plus every group Add has registered since
	async function refreshGroups(): Promise<void> {
		try {
			groups = await databaseState.listGroups();
		} catch {
			// a database that never came up already says so in both panels
		}
	}

	async function produce(): Promise<void> {
		producing = true;

		try {
			await databaseState.produce(produceDescription);
			produceError = null;
		} catch (caught) {
			produceError = caught instanceof Error ? caught.message : String(caught);
		} finally {
			producing = false;
		}
	}

	// a new group is a write -- registerGroup is what creates the cursor row it
	// replays from. Joining an existing group only adds a card: that group's
	// cursor is already there, and the two consumers claim off it in turn.
	async function addConsumer(group: string | null): Promise<void> {
		adding = true;

		try {
			const target = group ?? `group ${groups.length + 1}`;
			if (group === null) {
				await databaseState.registerGroup(target);
				await refreshGroups();
			}

			const name = `consumer ${nextConsumer}`;
			consumers.push({
				name,
				group: target,
				autoRun: true,
				lines: [{ kind: 'note', text: joinNote(group, target) }],
				status: noTicksYet,
			});
			autoRunner.start(name);
			nextConsumer += 1;
			addError = null;
		} catch (caught) {
			addError = caught instanceof Error ? caught.message : String(caught);
		} finally {
			adding = false;
		}
	}

	function joinNote(group: string | null, target: string): string {
		if (group === null) {
			return `its own cursor, still at 0 —\nits first run reads everything\nproduced so far.`;
		}
		return `shares ${target}'s cursor —\nits runs claim ranges the\nothers have not.`;
	}

	// one tick: claim a range off the group's cursor, hand each message inside it
	// to the handler, free the lease. The handler is this page's -- it succeeds
	// on every message and its only work is the line it writes to the card.
	// Nothing calls this but the consumer's own clock.
	async function tickConsumer(name: string): Promise<void> {
		const consumer = consumers.find((candidate) => candidate.name === name);
		if (consumer === undefined) return;

		try {
			const handled: ConsumerLine[] = [];
			const claimed = await databaseState.tick(consumer.group, (message) => {
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
			setAutoRun(name, false);
			consumer.status = {
				text: caught instanceof Error ? caught.message : String(caught),
				tone: 'error',
			};
		}
	}

	function setAutoRun(name: string, on: boolean): void {
		const consumer = consumers.find((candidate) => candidate.name === name);
		if (consumer === undefined) return;

		consumer.autoRun = on;
		if (on) {
			autoRunner.start(name);
			return;
		}
		autoRunner.stop(name);
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

	// the group and its cursor outlive the card: a group with no consumers
	// still holds its place in the log
	function removeConsumer(name: string): void {
		autoRunner.stop(name);
		consumers = consumers.filter((consumer) => consumer.name !== name);
	}
</script>

<div class="sandbox">
	<div class="title-bar">
		<span class="sandbox-label">{label}</span>
		<span class="sandbox-meta">postgres 18 · wasm · local to this tab</span>
		<ChromeButton
			label="Reset sandbox ↻"
			ariaLabel="Reset the sandbox"
			tone="primary"
			pressed={null}
			disabled={busy}
			onclick={() => void reset()}
		/>
	</div>
	<ProduceMessage
		{topic}
		text={produceDescription}
		errorMessage={produceError}
		disabled={busy || producing}
		ontext={(next) => (produceDescription = next)}
		onproduce={() => void produce()}
	/>
	<div class="panels">
		<SqlPanel {databaseState} panelShell={messages} />
		<SqlPanel {databaseState} panelShell={cursors} />
		{#if databaseState.status === 'connecting' && databaseState.stage !== null}
			<div class="progress-overlay">
				<DatabaseProgress stage={databaseState.stage} />
			</div>
		{/if}
	</div>
	<section class="consumer-region" aria-label="Consumers">
		<div class="consumers">
			<ConsumerGrid {consumers} disabled={busy} onautorun={setAutoRun} onremove={removeConsumer} />
			<AddConsumer
				{groups}
				errorMessage={addError}
				disabled={busy || adding}
				onadd={(group) => void addConsumer(group)}
			/>
		</div>
	</section>
</div>

<style src="./sandbox.css"></style>
