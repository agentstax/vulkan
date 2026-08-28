// CodeMirror assembly, reached only by dynamic import so the editor never
// rides in the initial payload. The theme mirrors the static shell's .sql
// styles so the swap-in causes zero layout shift.
import { EditorView, keymap } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
import { PostgreSQL, sql } from '@codemirror/lang-sql';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';

const editorTheme = EditorView.theme({
	'&': { backgroundColor: 'var(--color-console-sql-bg)', fontSize: '12.5px' },
	'&.cm-focused': { outline: 'none' },
	'.cm-scroller': { fontFamily: 'var(--font-database)', lineHeight: '1.6' },
	'.cm-content': { padding: '12px 0', caretColor: 'var(--color-text)' },
	'.cm-line': { padding: '0 14px' },
});

const keywordHighlight = HighlightStyle.define([
	{ tag: tags.keyword, color: 'var(--color-console-keyword)', fontWeight: '600' },
]);

export function createEditor(
	parent: HTMLElement,
	initialSql: string,
	onSqlChange: (sql: string) => void,
): EditorView {
	return new EditorView({
		parent,
		doc: initialSql,
		extensions: [
			history(),
			keymap.of([...defaultKeymap, ...historyKeymap]),
			sql({ dialect: PostgreSQL }),
			syntaxHighlighting(keywordHighlight),
			editorTheme,
			EditorView.updateListener.of((update) => {
				if (update.docChanged) onSqlChange(update.state.doc.toString());
			}),
		],
	});
}
