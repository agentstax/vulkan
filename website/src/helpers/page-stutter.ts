// The page's flinch when a reader presses Accept all -- hard blinks and a
// couple of scroll jolts, so something looks to have loaded underneath
// before the modal arrives. The blinks live in the global stylesheet
// because it is the page shuddering, not any one component; this switches
// them on, jolts the page, and reports when it is over.

// The shove is a scroll and not a transform on purpose: a transform on an
// ancestor makes it the containing block for every position: fixed
// descendant, which would fling the consent bar from the viewport bottom to
// the document bottom mid-animation. Scrolling moves the page for real.
const jolts = [
	{ atMs: 70, byPixels: -7 },
	{ atMs: 200, byPixels: 11 },
	{ atMs: 310, byPixels: -5 },
];

// if animationend never arrives -- a style the browser refused, a tab
// backgrounded mid-run -- the reveal still has to happen
const stutterFallbackMs = 900;

export function runPageStutter(): Promise<void> {
	if (typeof document === 'undefined' || prefersReducedMotion()) return Promise.resolve();

	return new Promise((resolve) => {
		const restingScroll = window.scrollY;
		const timers: number[] = [];
		let settled = false;

		// animationend bubbles, so a descendant's own animation ending
		// inside the window would otherwise cut the stutter short
		const finish = (event: AnimationEvent | null): void => {
			if (settled) return;
			if (event !== null && event.target !== document.body) return;

			settled = true;
			for (const timer of timers) {
				window.clearTimeout(timer);
			}

			document.body.removeEventListener('animationend', finish);
			document.body.removeAttribute('data-stutter');
			window.scrollTo({ top: restingScroll, behavior: 'instant' });
			resolve();
		};

		for (const jolt of jolts) {
			timers.push(
				window.setTimeout(() => {
					window.scrollBy({ top: jolt.byPixels, behavior: 'instant' });
				}, jolt.atMs),
			);
		}

		timers.push(window.setTimeout(() => finish(null), stutterFallbackMs));
		document.body.addEventListener('animationend', finish);
		document.body.dataset.stutter = '';
	});
}

// ***************
// *** HELPERS ***
// ***************

// a reader who asked for less motion goes straight to the modal: the
// stylesheet withholds the animation, so there is nothing to wait for
function prefersReducedMotion(): boolean {
	return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}
