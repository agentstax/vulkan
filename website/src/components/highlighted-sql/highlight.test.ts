import { describe, expect, it } from 'vitest';
import { sqlSegments } from './highlight';

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

	it('returns no segments for empty sql', () => {
		expect(sqlSegments('')).toEqual([]);
	});
});
