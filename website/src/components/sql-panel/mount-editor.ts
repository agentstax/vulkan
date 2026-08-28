// The editor swap: on idle, dynamic-import CodeMirror and put the editor in
// the host over the static shell. This module carries no CodeMirror of its
// own -- the type import erases -- so the panel imports it statically and the
// chunk still never rides the initial payload.
import type { EditorView } from '@codemirror/view';

// mounts into host and returns the cleanup that cancels a swap still waiting
// on idle, or destroys the editor a finished one put there
export function mountEditorOnIdle(
	host: HTMLElement,
	initialSql: string,
	onSqlChange: (sql: string) => void,
	onMounted: () => void,
	onLost: () => void,
): () => void {
	let editorView: EditorView | null = null;
	let cancelled = false;

	requestIdle(() => {
		void (async () => {
			try {
				const { createEditor } = await import('./editor');
				if (cancelled) return;

				editorView = createEditor(host, initialSql, onSqlChange);
				onMounted();
			} catch {
				// a lost chunk leaves the static shell in place, which still
				// reads correctly but it doesn't work
				if (cancelled) return;
				onLost();
			}
		})();
	});

	return () => {
		cancelled = true;
		editorView?.destroy();
	};
}

// ***************
// *** HELPERS ***
// ***************

// Safari gained requestIdleCallback in 18; elsewhere the next timer tick
// keeps the same "later, not now" effect
function requestIdle(callback: () => void): void {
	if (typeof window.requestIdleCallback === 'function') {
		window.requestIdleCallback(callback);
	} else {
		window.setTimeout(callback, 1);
	}
}
