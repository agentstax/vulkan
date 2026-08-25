<script lang="ts">
	import { onMount } from 'svelte';
	import AddConsumer from '../add-consumer/add-consumer.svelte';
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import DatabaseProgress from '../database-progress/database-progress.svelte';
	import type { Consumer, ConsumerLine } from '../consumer-card/types';
	import ConsumerGrid from '../consumer-grid/consumer-grid.svelte';
	import ProduceMessage from '../produce-message/produce-message.svelte';
	import SqlPanel from '../sql-panel/sql-panel.svelte';
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

	let produceText = $state('restock the ovens');
	let produceError: string | null = $state(null);
	let producing = $state(false);

	// consumer 1 comes with the page so the mechanism has an object to act on;
	// every other card is one the reader added
	let consumers: Consumer[] = $state([seededConsumer()]);
	let groups: string[] = $state([]);
	let addError: string | null = $state(null);
	let adding = $state(false);
	let ticking = $state(false);
	let resetting = $state(false);

	// never the card count: removing consumer 2 and adding again would reuse the
	// name, and the grid keys its cards by it
	let nextConsumer = 2;

	const databaseState = new DatabaseState();
	const busy = $derived(databaseState.status === 'connecting' || resetting);

	// the database starts booting as soon as the sandbox is on screen, so a
	// panel's first run only waits on the statement; a boot failure reaches
	// every panel through its own run, which is where it is reported
	onMount(() => {
		void databaseState.connect().catch(() => {});
		void refreshGroups();
	});

	// a page control, not an API verb: the database is dropped and rebuilt from
	// the seed, and the cards go with it -- their lines describe ticks against a
	// database that no longer exists.
	async function reset(): Promise<void> {
		resetting = true;

		try {
			await databaseState.reset();
			consumers = [seededConsumer()];
			nextConsumer = 2;
			produceError = null;
			addError = null;
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
			lines: [
				{
					kind: 'note',
					text: "billing's cursor is at 0 —\nits first tick claims from the\nstart of the log.",
				},
			],
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
			await databaseState.produce(produceText);
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

			consumers.push({
				name: `consumer ${nextConsumer}`,
				group: target,
				lines: [{ kind: 'note', text: joinNote(group, target) }],
			});
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
			return `its own cursor, still at 0 —\nits first tick reads everything\nproduced so far.`;
		}
		return `shares ${target}'s cursor —\nits ticks claim ranges the\nothers have not.`;
	}

	// one tick: claim a range off the group's cursor, hand each message inside it
	// to the handler, free the lease. The handler is this page's -- it succeeds
	// on every message and its only work is the line it writes to the card.
	async function tickConsumer(name: string): Promise<void> {
		const consumer = consumers.find((candidate) => candidate.name === name);
		if (consumer === undefined) return;

		ticking = true;

		try {
			const handled: ConsumerLine[] = [];
			const claimed = await databaseState.tick(consumer.group, (message) => {
				handled.push({ kind: 'handled', text: handledText(message), status: 'ok' });
			});

			if (claimed === null) {
				consumer.lines.push({ kind: 'note', text: 'caught up · nothing to claim' });
				return;
			}

			consumer.lines.push({
				kind: 'claim',
				text: claimText(claimed.low, claimed.high, handled.length),
			});
			consumer.lines.push(...handled);
		} catch (caught) {
			consumer.lines.push({
				kind: 'error',
				text: caught instanceof Error ? caught.message : String(caught),
			});
		} finally {
			ticking = false;
		}
	}

	// the range is the cursor's, the count is what came back inside it: a keyed
	// message a newer one on its key replaced is inside the range and unread
	function claimText(low: number, high: number, count: number): string {
		return `claim (${low}, ${high}] · ${count} ${count === 1 ? 'message' : 'messages'}`;
	}

	// the payload's own text field when it has one -- the reader typed it into
	// the produce strip -- and the whole payload otherwise
	function handledText(message: ClaimedMessage): string {
		const payload = message.payload;
		const text =
			typeof payload === 'object' && payload !== null && 'text' in payload ? payload.text : payload;
		return `#${message.id} ${JSON.stringify(text)}`;
	}

	// the group and its cursor outlive the card: a group with no consumers
	// still holds its place in the log
	function removeConsumer(name: string): void {
		consumers = consumers.filter((consumer) => consumer.name !== name);
	}
</script>

<div class="sandbox">
	<div class="title-bar">
		<span class="sandbox-label">{label}</span>
		<span class="sandbox-meta">postgres 18 · wasm · local to this tab</span>
		<ChromeButton
			label="Reset sandbox ↻"
			tone="primary"
			disabled={busy}
			onclick={() => void reset()}
		/>
	</div>
	<ProduceMessage
		{topic}
		text={produceText}
		errorMessage={produceError}
		disabled={busy || producing}
		ontext={(next) => (produceText = next)}
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
	<div class="consumers">
		<ConsumerGrid
			{consumers}
			disabled={busy || ticking}
			ontick={(name) => void tickConsumer(name)}
			onremove={removeConsumer}
		/>
		<AddConsumer
			{groups}
			errorMessage={addError}
			disabled={busy || adding}
			onadd={(group) => void addConsumer(group)}
		/>
	</div>
</div>

<style src="./sandbox.css"></style>
