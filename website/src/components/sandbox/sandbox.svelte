<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import AddConsumer from '../add-consumer/add-consumer.svelte';
	import BootNotice from '../boot-notice/boot-notice.svelte';
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import DatabaseProgress from '../database-progress/database-progress.svelte';
	import IslandBoundary from '../island-boundary/island-boundary.svelte';
	import ConsumerGrid from '../consumer-grid/consumer-grid.svelte';
	import ProduceMessage from '../produce-message/produce-message.svelte';
	import SqlPanel from '../sql-panel/sql-panel.svelte';
	import { SandboxState } from './sandbox-state.svelte';
	import type { PanelShell } from './types';

	type Props = {
		topic: string;
		messages: PanelShell;
		cursors: PanelShell;
	};

	let { topic, messages, cursors }: Props = $props();

	const sandboxState = new SandboxState();

	onMount(() => sandboxState.connect());
	onDestroy(() => sandboxState.close());
</script>

<IslandBoundary name="sandbox">
	<div class="sandbox">
		<div class="title-bar">
			<span class="sandbox-meta">postgres 18 · wasm · local to this tab</span>
			<ChromeButton
				label="Reset sandbox ↻"
				ariaLabel="Reset the sandbox"
				tone="primary"
				pressed={null}
				disabled={sandboxState.busy}
				onclick={() => void sandboxState.reset()}
			/>
		</div>
		<ProduceMessage
			{topic}
			text={sandboxState.produceDescription}
			errorMessage={sandboxState.produceError}
			disabled={sandboxState.busy || sandboxState.bootFailed || sandboxState.producing}
			ontext={(next) => (sandboxState.produceDescription = next)}
			onproduce={() => void sandboxState.produce()}
		/>
		<div class="panels">
			<SqlPanel databaseState={sandboxState.databaseState} panelShell={messages} />
			<SqlPanel databaseState={sandboxState.databaseState} panelShell={cursors} />
			{#if sandboxState.databaseState.status === 'connecting' && sandboxState.databaseState.stage !== null}
				<div class="progress-overlay">
					<DatabaseProgress stage={sandboxState.databaseState.stage} />
				</div>
			{:else if sandboxState.bootFailed}
				<div class="progress-overlay">
					<BootNotice />
				</div>
			{/if}
		</div>
		<section class="consumer-region" aria-label="Consumers">
			<div class="consumers">
				<ConsumerGrid
					consumers={sandboxState.consumers}
					disabled={sandboxState.busy || sandboxState.bootFailed}
					onautorun={(name, on) => sandboxState.setAutoRun(name, on)}
					onremove={(name) => sandboxState.removeConsumer(name)}
				/>
				<AddConsumer
					groups={sandboxState.groups}
					errorMessage={sandboxState.addError}
					disabled={sandboxState.busy || sandboxState.bootFailed || sandboxState.adding}
					onadd={(group) => void sandboxState.addConsumer(group)}
				/>
			</div>
		</section>
	</div>
</IslandBoundary>

<style src="./sandbox.css"></style>
