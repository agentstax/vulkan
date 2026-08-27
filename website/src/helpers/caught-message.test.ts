import { describe, expect, it } from 'vitest';
import { caughtMessage } from './caught-message';

const fallback = 'we done goofed it up — reload the page to try again';

describe('caughtMessage', () => {
	it('returns an Error value message', () => {
		expect(caughtMessage(new Error('relation "delivery" does not exist'))).toBe(
			'relation "delivery" does not exist',
		);
	});

	it('returns a thrown string as it is', () => {
		expect(caughtMessage('boot interrupted')).toBe('boot interrupted');
	});

	it('falls back for an Error with an empty message', () => {
		expect(caughtMessage(new Error())).toBe(fallback);
	});

	it('falls back for an empty string', () => {
		expect(caughtMessage('')).toBe(fallback);
	});

	it('falls back for a thrown object', () => {
		expect(caughtMessage({ code: 'XX000' })).toBe(fallback);
	});

	it('falls back for null and undefined', () => {
		expect(caughtMessage(null)).toBe(fallback);
		expect(caughtMessage(undefined)).toBe(fallback);
	});
});
