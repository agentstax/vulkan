<script lang="ts">
	import { onMount } from 'svelte';
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import ConsoleProgress from '../console-progress/console-progress.svelte';
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

	const databaseState = new DatabaseState();
	const busy = $derived(databaseState.status === 'connecting');

	// the database starts booting as soon as the console is on screen, so a
	// panel's first run only waits on the statement; a boot failure reaches
	// every panel through its own run, which is where it is reported
	onMount(() => {
		void databaseState.connect().catch(() => {});
	});
</script>

<div class="sql-console">
	<div class="title-bar">
		<span class="console-label">{label}</span>
		<span class="console-meta">postgres 18 · wasm · local to this tab</span>
		<!-- the click does nothing yet: dropping the database and rebuilding it
		     from the seed is not wired up -->
		<ChromeButton label="Reset sandbox ↻" disabled={busy} onclick={() => {}} />
	</div>
	<ProduceStrip {topic} text={produceText} ontext={(next) => (produceText = next)} />
	<div class="panels">
		<SqlPanel {databaseState} panelShell={messages} editable={true} />
		<SqlPanel {databaseState} panelShell={cursors} editable={false} />
		{#if databaseState.status === 'connecting' && databaseState.stage !== null}
			<div class="progress-overlay">
				<ConsoleProgress stage={databaseState.stage} />
			</div>
		{/if}
	</div>
</div>

<style src="./sql-console.css"></style>
