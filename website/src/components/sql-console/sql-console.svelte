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

	// example cards -- no consumer runs yet, and nothing here reads the database
	const consumers: Consumer[] = [
		{
			name: 'consumer 1',
			group: 'billing',
			lines: [
				{ kind: 'claim', text: 'claim (0, 1]' },
				{ kind: 'handled', text: '"ship order 4471"', status: 'ok' },
				{ kind: 'claim', text: 'claim (1, 2]' },
				{ kind: 'handled', text: '"refund order 4468"', status: 'ok' },
			],
		},
		{
			name: 'consumer 2',
			group: 'billing',
			lines: [
				{
					kind: 'note',
					text: 'same group as consumer 1.\nits next tick claims (2, 3] —\nranges never overlap.',
				},
			],
		},
		{
			name: 'consumer 3',
			group: 'search',
			lines: [
				{
					kind: 'note',
					text: 'its own cursor, still at 0 —\nits first tick reads all three\nmessages billing handled.',
				},
			],
		},
	];

	// the groups a new consumer can join are the ones the cards already name
	const groups = [...new Set(consumers.map((consumer) => consumer.group))];

	const databaseState = new DatabaseState();
	const busy = $derived(databaseState.status === 'connecting');

	// the database starts booting as soon as the console is on screen, so a
	// panel's first run only waits on the statement; a boot failure reaches
	// every panel through its own run, which is where it is reported
	onMount(() => {
		void databaseState.connect().catch(() => {});
	});

	// the row lands immediately; re-reading message_log_1 is still the panel's
	// own Run
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
		<!-- the clicks do nothing yet: a tick claiming, handling and committing
		     one message is not wired up, and neither is registering a group -->
		<ConsumerGrid {consumers} ontick={() => {}} onremove={() => {}} />
		<AddConsumer {groups} onadd={() => {}} />
	</div>
</div>

<style src="./sql-console.css"></style>
