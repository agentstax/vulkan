import { caughtMessage } from '../helpers/caught-message';

// The page-level notice: the surface for failures no island caught. One
// notice shows at a time; a face never replaces a more demanding one.
const reloadedKey = 'vulkan-board:chunk-reload';

// banner: a fault the reader can wave away; the page keeps working
// modal: a reload is the fix and the reader must choose
export type SiteNoticeKind = 'banner' | 'modal';

export type SiteNotice = {
	kind: SiteNoticeKind;
	detail: string | null;
};

const noticeRank: Record<SiteNoticeKind, number> = { banner: 0, modal: 1 };

export class SiteNoticeState {
	current: SiteNotice | null = $state(null);

	// the detail the reader last sent away; the same fact re-raised is not
	// re-shown
	private dismissedDetail: string | null = null;

	show(kind: SiteNoticeKind, detail: string | null): void {
		if (detail !== null && detail === this.dismissedDetail) return;
		if (this.current !== null && noticeRank[this.current.kind] > noticeRank[kind]) return;
		this.current = { kind, detail };
	}

	// the reader's dismissal: the notice goes and its detail stays suppressed
	dismiss(): void {
		if (this.current === null) return;
		this.dismissedDetail = this.current.detail;
		this.current = null;
	}

	// drops the notice without recording a dismissal -- for the page leaving,
	// not the reader deciding
	clear(): void {
		this.current = null;
	}
}

export const siteNotice = new SiteNoticeState();

let listening = false;

// Called from BoardLayout's bundled script. A bundled module runs once and
// window survives ClientRouter swaps, so the listeners register exactly once
// for the whole visit; the flag is insurance against a second call site.
export function listenForPageFailures(): void {
	if (listening) return;
	listening = true;

	// the two throws no try/catch or boundary saw: sync errors with no
	// caller, and rejections nothing awaited
	window.addEventListener('error', (event) => {
		siteNotice.show('banner', caughtMessage(event.error ?? event.message));
	});

	window.addEventListener('unhandledrejection', (event) => {
		siteNotice.show('banner', caughtMessage(event.reason));
	});

	// a failed chunk after a redeploy: fresh HTML names fresh chunks, so one
	// reload is the fix; the second failure in a session asks instead
	window.addEventListener('vite:preloadError', (event) => {
		if (markReloadUsed()) {
			event.preventDefault();
			window.location.reload();
			return;
		}
		siteNotice.show('modal', null);
	});

	// a banner about the page being left is stale on the next one; a modal
	// or full-page notice still wants its answer
	document.addEventListener('astro:before-swap', () => {
		if (siteNotice.current !== null && siteNotice.current.kind === 'banner') {
			siteNotice.clear();
		}
	});
}

// ***************
// *** HELPERS ***
// ***************

// one automatic reload per tab session -- a chunk lost to a bad network
// instead of a redeploy would otherwise reload forever
function markReloadUsed(): boolean {
	try {
		if (window.sessionStorage.getItem(reloadedKey) !== null) return false;
		window.sessionStorage.setItem(reloadedKey, '1');
		return true;
	} catch {
		// storage denied -- with no way to bound reloads, do not start one
		return false;
	}
}
