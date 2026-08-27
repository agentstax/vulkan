// The sandbox's timers. A consumer instance with auto-run on runs about once a
// second on its own clock; this class owns those clocks and nothing else -- the
// run itself, and the flag the card renders, belong to the sandbox.
const baseDelayMs = 1000;
const jitterMs = 300;

export class AutoRunner {
	private run: (name: string) => Promise<void>;

	// the consumers whose auto-run is on. A pending timeout is not the same
	// fact: a run in flight has no timer, and turning auto-run off mid-run has
	// to stop the next one
	private on = new Set<string>();
	private timers = new Map<string, ReturnType<typeof setTimeout>>();

	constructor(run: (name: string) => Promise<void>) {
		this.run = run;
	}

	start(name: string): void {
		if (this.on.has(name)) return;
		this.on.add(name);
		this.schedule(name);
	}

	stop(name: string): void {
		this.on.delete(name);
		const pending = this.timers.get(name);
		if (pending === undefined) return;
		clearTimeout(pending);
		this.timers.delete(name);
	}

	stopAll(): void {
		for (const name of [...this.on]) {
			this.stop(name);
		}
	}

	private schedule(name: string): void {
		const timer = setTimeout(() => void this.fire(name), nextDelay());
		this.timers.set(name, timer);
	}

	// the next run is scheduled only once this one has finished, so a slow run
	// delays the one after it instead of stacking a second claim on top
	private async fire(name: string): Promise<void> {
		this.timers.delete(name);

		try {
			await this.run(name);
		} catch {
			// the run reports through the card it belongs to; a throw that
			// escaped it must not end the clock, or the card freezes silently
		}

		// auto-run went off, or the card was removed, while the run was in
		// flight: this consumer has no next run
		if (!this.on.has(name)) return;
		this.schedule(name);
	}
}

// ***************
// *** HELPERS ***
// ***************

// what one consumer waits between runs. Consumer instances poll on their own
// clocks; without the jitter every card started in the same moment stays in
// lockstep forever, which reads as one timer driving all of them.
function nextDelay(): number {
	return baseDelayMs + (Math.random() - 0.5) * jitterMs;
}
