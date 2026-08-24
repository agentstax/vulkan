<script lang="ts">
	import { onMount } from 'svelte';
	import type { EditorView } from '@codemirror/view';
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import HighlightedSql from '../highlighted-sql/highlighted-sql.svelte';
	import type { DatabaseState } from '../sql-console/database-state.svelte';
	import type { PanelShell } from '../sql-console/types';
	import SqlResult from '../sql-result/sql-result.svelte';
	import { PanelState } from './sql-panel-state.svelte';

	type Props = {
		databaseState: DatabaseState;
		panelShell: PanelShell;
		editable: boolean;
	};

	let { databaseState, panelShell, editable }: Props = $props();

	let editorHost: HTMLDivElement | undefined = $state(undefined);
	let editorMounted = $state(false);

	// the shell seeds the panel once -- after that the editor owns the SQL and
	// runs own the results, so reading the prop at construction is the intent
	// svelte-ignore state_referenced_locally
	const panelState = new PanelState(databaseState, panelShell);
	const runDisabled = $derived(databaseState.status === 'connecting' || panelState.running);

	// the panel's own read of the database: once when it mounts, and again each
	// time a write bumps the revision. The seeded shell rows hold the table
	// until that first result lands.
	$effect(() => {
		panelState.runAt(databaseState.revision);
	});

	onMount(() => {
		if (!editable) return;

		let editorView: EditorView | null = null;
		let cancelled = false;

		// Safari gained requestIdleCallback in 18; elsewhere the next timer
		// tick keeps the same "later, not now" effect
		function requestIdle(callback: () => void): void {
			if (typeof window.requestIdleCallback === 'function') {
				window.requestIdleCallback(callback);
			} else {
				window.setTimeout(callback, 1);
			}
		}

		// the initial doc seeds the editor once -- after that the editor owns
		// the text and pushes it back into the panel state
		const initialSql = panelState.sql;

		// on idle, dynamic-import CodeMirror and swap it in over the static
		// shell -- the editor never rides the initial payload
		requestIdle(() => {
			void (async () => {
				const { createEditor } = await import('../sql-console/editor');

				if (cancelled || editorHost === undefined) return;

				editorView = createEditor(editorHost, initialSql, (next) => (panelState.sql = next));
				editorMounted = true;
			})();
		});

		return () => {
			cancelled = true;
			editorView?.destroy();
		};
	});
</script>

<div class="sql-panel">
	<div class="panel-bar">
		<span class="panel-name">{panelState.table}</span>
		<span class="panel-chip">auto re-runs</span>
		<ChromeButton
			label="Run ▸"
			tone="primary"
			disabled={runDisabled}
			onclick={() => void panelState.run()}
		/>
	</div>
	<div class="editor-area">
		<!-- static SQL text holds this spot until the idle callback puts the
		     CodeMirror editor in editorHost -- identically sized, so the swap
		     causes no layout shift -->
		{#if !editorMounted}
			<HighlightedSql sql={panelState.sql} />
		{/if}
		<div class="editor-host" bind:this={editorHost}></div>
	</div>
	<div class="result-area">
		{#if panelState.errorMessage !== null}
			<div class="error-panel" role="alert">{panelState.errorMessage}</div>
		{:else}
			<SqlResult result={panelState.result} />
		{/if}
	</div>
</div>

<style src="./sql-panel.css"></style>
