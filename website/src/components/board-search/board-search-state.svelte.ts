import { excerptSegments, type ExcerptSegment } from '../../helpers/excerpt-segments';

// ready: nothing searched yet
// searching: a query is running
// done: the last query's hits are showing
// unavailable: the Pagefind bundle is missing (the site build generates it)
// stopped: the last query threw before finishing; running it again is fine
export type SearchPhase = 'ready' | 'searching' | 'done' | 'unavailable' | 'stopped';

export type SearchHit = {
	title: string;
	href: string;
	excerpt: ExcerptSegment[];
};

type PagefindResult = {
	data: () => Promise<{ url: string; excerpt: string; meta: Record<string, string> }>;
};

type Pagefind = {
	init: () => Promise<void>;
	debouncedSearch: (query: string) => Promise<{ results: PagefindResult[] } | null>;
};

const hitLimit = 20;

export class SearchState {
	query = $state('');
	phase: SearchPhase = $state('ready');
	hits: SearchHit[] = $state([]);
	totalCount = $state(0);
	private pagefind: Pagefind | null = null;

	async search(): Promise<void> {
		const query = this.query.trim();
		if (query === '') {
			return;
		}

		this.phase = 'searching';
		const pagefind = await this.load();
		if (pagefind === null) {
			this.phase = 'unavailable';
			return;
		}

		try {
			const found = await pagefind.debouncedSearch(query);
			// null = a newer search superseded this one; its own result is coming
			if (found === null) {
				return;
			}

			const pages = await Promise.all(
				found.results.slice(0, hitLimit).map((result) => result.data()),
			);
			this.hits = pages.map((page) => ({
				title: page.meta.title ?? page.url,
				href: page.url,
				excerpt: excerptSegments(page.excerpt),
			}));
			this.totalCount = found.results.length;
			this.phase = 'done';
		} catch {
			// without this, the throw leaves "searching…" up for good
			this.phase = 'stopped';
		}
	}

	private async load(): Promise<Pagefind | null> {
		if (this.pagefind !== null) {
			return this.pagefind;
		}

		// the bundle exists only in the built site; the variable keeps the
		// import opaque to the bundler so it stays a runtime one
		try {
			const bundlePath = '/pagefind/pagefind.js';
			const pagefind = (await import(/* @vite-ignore */ bundlePath)) as Pagefind;
			await pagefind.init();
			this.pagefind = pagefind;
			return pagefind;
		} catch {
			return null;
		}
	}
}
