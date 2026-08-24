<script lang="ts">
	import { onMount } from 'svelte';
	import type { EditorView } from '@codemirror/view';
	import { ConsoleState } from './sql-console-state.svelte';
	import HighlightedSql from '../highlighted-sql/highlighted-sql.svelte';
	import SqlResult from '../sql-result/sql-result.svelte';
	import ConsoleProgress from '../console-progress/console-progress.svelte';

	type Props = {
		label: string;
		sql: string;
		columns: string[];
		rows: (string | null)[][];
	};

	let { label, sql, columns, rows }: Props = $props();

	// the props seed the state once -- after that the editor owns the SQL
	// and runs own the result, so capturing initial values is the intent
	// svelte-ignore state_referenced_locally
	const consoleState = new ConsoleState(sql, columns, rows);
	const runDisabled = $derived(
		consoleState.phase === 'shell' ||
			consoleState.phase === 'connecting' ||
			consoleState.phase === 'running',
	);
	let editorHost: HTMLDivElement | undefined = $state(undefined);

	// on idle, dynamic-import CodeMirror and swap it in over the static shell
	// (enabling Run) -- neither editor nor database rides the initial payload
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

		requestIdle(() => {
			void (async () => {
				const { createEditor } = await import('./editor');

				if (cancelled || editorHost === undefined) return;

				editorView = createEditor(editorHost, consoleState.sql, (nextSql) => {
					consoleState.sql = nextSql;
				});
				consoleState.editorReady();

				// warm the database chunk so first Run only pays the wasm start
				requestIdle(() => void import('./database'));
			})();
		});

		return () => {
			cancelled = true;
			editorView?.destroy();
		};
	});
</script>

<div class="sql-console">
	<div class="title-bar">
		<span class="console-label">{label}</span>
		<span class="console-meta">postgres 18 · wasm · local to this tab</span>
		<button
			type="button"
			class="run-button"
			disabled={runDisabled}
			onclick={() => void consoleState.run()}
		>
			Run ▸
		</button>
	</div>
	<div class="editor-area">
		<!-- static SQL text holds this spot until onMount's idle callback puts
		     the CodeMirror editor in editorHost -- identically sized, so the
		     swap causes no layout shift; leaving phase 'shell' unmounts the text -->
		{#if consoleState.phase === 'shell'}
			<HighlightedSql sql={consoleState.sql} />
		{/if}
		<div class="editor-host" bind:this={editorHost}></div>
	</div>
	<div class="result-area">
		{#if consoleState.phase === 'error' && consoleState.errorMessage !== null}
			<div class="error-panel" role="alert">{consoleState.errorMessage}</div>
		{:else}
			<SqlResult result={consoleState.result} />
		{/if}
		{#if consoleState.phase === 'connecting' && consoleState.stage !== null}
			<div class="progress-overlay">
				<ConsoleProgress stage={consoleState.stage} />
			</div>
		{/if}
	</div>
</div>

<style src="./sql-console.css"></style>
