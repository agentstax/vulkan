<script lang="ts">
	import { onMount } from 'svelte';
	import AddConsumer from '../add-consumer/add-consumer.svelte';
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import ConsoleProgress from '../console-progress/console-progress.svelte';
	import type { Consumer } from '../consumer-card/types';
	import ConsumerGrid from '../consumer-grid/consumer-grid.svelte';
	import ProduceStrip from '../produce-strip/produce-strip.svelte';
	import SqlPanel from '../sql-panel/sql-panel.svelte';
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
	let consumers: Consumer[] = $state([
		{
			name: 'consumer 1',
			group: 'billing',
			lines: [
				{
					kind: 'note',
					text: "billing's cursor is at 0 \u2014\nits first tick claims from the\nstart of the log.",
				},
			],
		},
	]);
	let groups: string[] = $state([]);
	let addError: string | null = $state(null);
	let adding = $state(false);

	// never the card count: removing consumer 2 and adding again would reuse the
	// name, and the grid keys its cards by it
	let nextConsumer = 2;

	const databaseState = new DatabaseState();
	const busy = $derived(databaseState.status === 'connecting');

	// the database starts booting as soon as the console is on screen, so a
	// panel's first run only waits on the statement; a boot failure reaches
	// every panel through its own run, which is where it is reported
	onMount(() => {
		void databaseState.connect().catch(() => {});
		void refreshGroups();
	});

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

	// the group and its cursor outlive the card: a group with no consumers
	// still holds its place in the log
	function removeConsumer(name: string): void {
		consumers = consumers.filter((consumer) => consumer.name !== name);
	}
</script>

<div class="sql-console">
	<div class="title-bar">
		<span class="console-label">{label}</span>
		<span class="console-meta">postgres 18 · wasm · local to this tab</span>
		<!-- the click does nothing yet: dropping the database and rebuilding it
		     from the seed is not wired up -->
		<ChromeButton label="Reset sandbox ↻" tone="primary" disabled={busy} onclick={() => {}} />
	</div>
	<ProduceStrip
		{topic}
		text={produceText}
		errorMessage={produceError}
		disabled={busy || producing}
		ontext={(next) => (produceText = next)}
		onproduce={() => void produce()}
	/>
	<div class="panels">
		<SqlPanel {databaseState} panelShell={messages} editable={true} />
		<SqlPanel {databaseState} panelShell={cursors} editable={false} />
		{#if databaseState.status === 'connecting' && databaseState.stage !== null}
			<div class="progress-overlay">
				<ConsoleProgress stage={databaseState.stage} />
			</div>
		{/if}
	</div>
	<div class="consumers">
		<!-- Tick does nothing yet: claiming, handling and committing one message
		     is not wired up -->
		<ConsumerGrid {consumers} ontick={() => {}} onremove={removeConsumer} />
		<AddConsumer
			{groups}
			errorMessage={addError}
			disabled={busy || adding}
			onadd={(group) => void addConsumer(group)}
		/>
	</div>
</div>

<style src="./sql-console.css"></style>
