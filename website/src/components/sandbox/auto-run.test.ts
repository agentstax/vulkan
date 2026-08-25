import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AutoRunner } from './auto-run';

// past the longest delay nextDelay can return, so advancing by it always
// reaches the next run whatever the jitter drew
const pastOneDelay = 1200;

describe('AutoRunner', () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it('runs the started consumer again and again', async () => {
		const runs: string[] = [];
		const runner = new AutoRunner(async (name) => {
			runs.push(name);
		});

		runner.start('consumer 1');
		await vi.advanceTimersByTimeAsync(pastOneDelay * 3);
		runner.stopAll();

		expect(runs).toEqual(['consumer 1', 'consumer 1', 'consumer 1']);
	});

	it('runs nothing until started', async () => {
		const runs: string[] = [];
		const runner = new AutoRunner(async (name) => {
			runs.push(name);
		});

		runner.stopAll();
		await vi.advanceTimersByTimeAsync(pastOneDelay * 3);

		expect(runs).toEqual([]);
	});

	it('gives each consumer its own clock', async () => {
		const runs: string[] = [];
		const runner = new AutoRunner(async (name) => {
			runs.push(name);
		});

		runner.start('consumer 1');
		runner.start('consumer 2');
		await vi.advanceTimersByTimeAsync(pastOneDelay);
		runner.stopAll();

		expect(runs.toSorted()).toEqual(['consumer 1', 'consumer 2']);
	});

	it('starts one clock for a consumer already started', async () => {
		const runs: string[] = [];
		const runner = new AutoRunner(async (name) => {
			runs.push(name);
		});

		runner.start('consumer 1');
		runner.start('consumer 1');
		await vi.advanceTimersByTimeAsync(pastOneDelay);
		runner.stopAll();

		expect(runs).toEqual(['consumer 1']);
	});

	it('stops the consumer named and leaves the others running', async () => {
		const runs: string[] = [];
		const runner = new AutoRunner(async (name) => {
			runs.push(name);
		});

		runner.start('consumer 1');
		runner.start('consumer 2');
		runner.stop('consumer 1');
		await vi.advanceTimersByTimeAsync(pastOneDelay * 2);
		runner.stopAll();

		expect(runs).toEqual(['consumer 2', 'consumer 2']);
	});

	// the reschedule is the tail of the run, so a stop that lands while the run
	// is in flight has to be seen after it resolves
	it('schedules no next run when stopped mid-run', async () => {
		let runs = 0;
		let release = (): void => {};
		const runner = new AutoRunner(async () => {
			runs += 1;
			await new Promise<void>((resolve) => {
				release = resolve;
			});
		});

		runner.start('consumer 1');
		await vi.advanceTimersByTimeAsync(pastOneDelay);
		runner.stop('consumer 1');
		release();
		await vi.advanceTimersByTimeAsync(pastOneDelay * 2);

		expect(runs).toBe(1);
	});
});
