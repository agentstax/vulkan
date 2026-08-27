<script lang="ts">
	import { onMount } from 'svelte';
	import type { EditorView } from '@codemirror/view';
	import ChromeButton from '../chrome-button/chrome-button.svelte';
	import HighlightedSql from '../highlighted-sql/highlighted-sql.svelte';
	import type { DatabaseState } from '../sandbox/database-state.svelte';
	import type { PanelShell } from '../sandbox/types';
	import SqlResult from '../sql-result/sql-result.svelte';
	import { PanelState } from './sql-panel-state.svelte';

	type Props = {
		databaseState: DatabaseState;
		panelShell: PanelShell;
	};

	let { databaseState, panelShell }: Props = $props();

	let editorHost: HTMLDivElement | undefined = $state(undefined);
	let editorMounted = $state(false);
	let editorMessage: string | null = $state(null);

	// the shell seeds the panel once -- after that the editor owns the SQL and
	// runs own the results, so reading the prop at construction is the intent
	// svelte-ignore state_referenced_locally
	const panelState = new PanelState(databaseState, panelShell);
	const runDisabled = $derived(databaseState.status === 'connecting' || panelState.running);

	// what the panel promises about its own results: it re-runs itself until the
	// visitor edits the query, and then says so rather than going quiet
	const chip = $derived(
		!panelState.edited ? 'auto re-runs' : panelState.stale ? 'edited · behind' : 'edited',
	);

	// the panel's own read of the database: once when it mounts, and again each
	// time a write bumps the revision. The seeded shell rows hold the table
	// until that first result lands.
	$effect(() => {
		panelState.runAt(databaseState.revision);
	});

	onMount(() => {
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
				try {
					const { createEditor } = await import('../sandbox/editor');

					if (cancelled || editorHost === undefined) return;

					editorView = createEditor(editorHost, initialSql, (next) => panelState.setSql(next));
					editorMounted = true;
				} catch {
					// a lost chunk leaves the static shell in place, which still
					// reads correctly but it doesn't work
					if (cancelled) return;
					editorMessage =
						'the editor could not load — the query still runs as shown; reload the page to edit it';
				}
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
		<span class="panel-chip" data-state={panelState.stale ? 'behind' : 'current'}>{chip}</span>
		<ChromeButton
			label="Run ▸"
			ariaLabel="Run this panel's query"
			tone="primary"
			pressed={null}
			disabled={runDisabled}
			onclick={() => void panelState.run()}
		/>
	</div>
	<div class="editor-area">
		<!-- static SQL text holds this spot until the idle callback puts the
		     CodeMirror editor in editorHost -- identically sized, so the swap
		     causes no layout shift -->
		{#if !editorMounted}
			<HighlightedSql sql={panelState.sql} values={new Map()} />
		{/if}
		<div class="editor-host" bind:this={editorHost}></div>
		{#if editorMessage !== null}
			<div class="editor-notice" role="alert">{editorMessage}</div>
		{/if}
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
