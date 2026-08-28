import { describe, expect, it } from 'vitest';
import { fillSegments, sqlSegments } from './highlight';

describe('sqlSegments', () => {
	it('returns one plain segment for text with no keywords', () => {
		expect(sqlSegments('the topic id')).toEqual([{ text: 'the topic id', kind: 'plain' }]);
	});

	it('splits keywords out of the surrounding text', () => {
		expect(sqlSegments('SELECT id FROM topic')).toEqual([
			{ text: 'SELECT', kind: 'keyword' },
			{ text: ' id ', kind: 'plain' },
			{ text: 'FROM', kind: 'keyword' },
			{ text: ' topic', kind: 'plain' },
		]);
	});

	it('marks a placeholder inside an identifier without breaking the table name', () => {
		expect(sqlSegments('FROM delivery_{topic_id}')).toEqual([
			{ text: 'FROM', kind: 'keyword' },
			{ text: ' delivery_', kind: 'plain' },
			{ text: '{topic_id}', kind: 'placeholder' },
		]);
	});

	it('keeps keywords highlighted on both sides of a placeholder', () => {
		expect(sqlSegments('WHERE message_id = {message_id} AND status = 1')).toEqual([
			{ text: 'WHERE', kind: 'keyword' },
			{ text: ' message_id = ', kind: 'plain' },
			{ text: '{message_id}', kind: 'placeholder' },
			{ text: ' ', kind: 'plain' },
			{ text: 'AND', kind: 'keyword' },
			{ text: ' status = 1', kind: 'plain' },
		]);
	});

	it('leaves a brace run that names no attribute as plain text', () => {
		expect(sqlSegments("payload @> '{}'")).toEqual([{ text: "payload @> '{}'", kind: 'plain' }]);
	});

	it('marks a -- comment to the end of its line', () => {
		expect(sqlSegments('-- your queue, selected\nSELECT id')).toEqual([
			{ text: '-- your queue, selected', kind: 'comment' },
			{ text: '\n', kind: 'plain' },
			{ text: 'SELECT', kind: 'keyword' },
			{ text: ' id', kind: 'plain' },
		]);
	});

	it('leaves a keyword and a brace run inside a comment as comment text', () => {
		expect(sqlSegments('-- select {topic_id} first')).toEqual([
			{ text: '-- select {topic_id} first', kind: 'comment' },
		]);
	});

	it('returns no segments for empty sql', () => {
		expect(sqlSegments('')).toEqual([]);
	});
});

describe('fillSegments', () => {
	it('substitutes an identifier position bare', () => {
		const filled = fillSegments(
			sqlSegments('FROM delivery_{topic_id}'),
			new Map([['topic_id', '7']]),
		);

		expect(filled).toEqual([
			{ text: 'FROM', kind: 'keyword' },
			{ text: ' delivery_', kind: 'plain' },
			{ text: '7', kind: 'value' },
		]);
	});

	it('substitutes a quoted position without adding quotes of its own', () => {
		const filled = fillSegments(
			sqlSegments("WHERE name = '{topic}';"),
			new Map([['topic', 'orders']]),
		);

		expect(filled).toEqual([
			{ text: 'WHERE', kind: 'keyword' },
			{ text: " name = '", kind: 'plain' },
			{ text: 'orders', kind: 'value' },
			{ text: "';", kind: 'plain' },
		]);
	});

	// the value closes the literal early otherwise, and the reader pastes SQL
	// that does not run
	it('doubles a quote inside a text literal', () => {
		const filled = fillSegments(
			sqlSegments("WHERE name = '{topic}';"),
			new Map([['topic', "o'brien"]]),
		);

		expect(filled[2]).toEqual({ text: "o''brien", kind: 'value' });
	});

	it('leaves a bare position alone when the value is not an identifier', () => {
		const filled = fillSegments(
			sqlSegments('WHERE id = {message_id};'),
			new Map([['message_id', '1; DROP TABLE topic']]),
		);

		expect(filled[2]).toEqual({ text: '{message_id}', kind: 'placeholder' });
	});

	it('leaves a placeholder nothing filled as a blank', () => {
		const filled = fillSegments(sqlSegments('FROM delivery_{topic_id}'), new Map());

		expect(filled[2]).toEqual({ text: '{topic_id}', kind: 'placeholder' });
	});
});
