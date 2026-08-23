// ready: the label invites the copy
// copied / failed: the outcome shows briefly, then the label returns
export type CopyPhase = 'ready' | 'copied' | 'failed';

const outcomeDisplayMs = 1600;

export class CopyButtonState {
	phase: CopyPhase = $state('ready');
	private generation = 0;

	async copy(text: string): Promise<void> {
		const generation = ++this.generation;
		try {
			await navigator.clipboard.writeText(text);
			this.phase = 'copied';
		} catch {
			this.phase = 'failed';
		}

		// a click during the outcome window starts a new generation; the
		// stale timer must not reset the newer outcome
		setTimeout(() => {
			if (generation === this.generation) this.phase = 'ready';
		}, outcomeDisplayMs);
	}
}
