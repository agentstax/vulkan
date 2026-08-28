import { describe, expect, it } from 'vitest';
import { newLeakedAccount } from './leaked-account';

// the generator is random, so every claim is checked over a run of them
const runs = 500;

function routingChecksum(routingNumber: string): number {
	const weights = [3, 7, 1, 3, 7, 1, 3, 7, 1];
	let sum = 0;
	for (let index = 0; index < weights.length; index++) {
		sum += (weights[index] ?? 0) * Number(routingNumber[index]);
	}

	return sum % 10;
}

describe('newLeakedAccount', () => {
	it('gives a nine digit routing number', () => {
		for (let run = 0; run < runs; run++) {
			expect(newLeakedAccount().routingNumber).toMatch(/^\d{9}$/);
		}
	});

	it('gives a routing number that passes the ABA check', () => {
		for (let run = 0; run < runs; run++) {
			expect(routingChecksum(newLeakedAccount().routingNumber)).toBe(0);
		}
	});

	it('gives a routing number whose prefix names a real district', () => {
		for (let run = 0; run < runs; run++) {
			const prefix = Number(newLeakedAccount().routingNumber.slice(0, 2));
			const named =
				(prefix >= 1 && prefix <= 12) ||
				(prefix >= 21 && prefix <= 32) ||
				(prefix >= 61 && prefix <= 72) ||
				prefix === 80;
			expect(named).toBe(true);
		}
	});

	it('gives a twelve digit account number', () => {
		for (let run = 0; run < runs; run++) {
			expect(newLeakedAccount().accountNumber).toMatch(/^\d{12}$/);
		}
	});

	it('does not repeat itself', () => {
		const seen = new Set<string>();
		for (let run = 0; run < runs; run++) {
			const leaked = newLeakedAccount();
			seen.add(`${leaked.routingNumber}:${leaked.accountNumber}`);
		}

		expect(seen.size).toBe(runs);
	});
});
