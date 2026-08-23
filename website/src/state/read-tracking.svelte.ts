// Read tracking is one append-only log of page visits: each entry is the
// page's href and when it was seen. Everything else derives from the log --
// the visit bar reads the stamp at the end, a scope counts as visited by
// its newest matching entry, and a folder glows amber while its scope holds
// a change newer than that. A browser with an empty log sees everything
// amber -- it is all new.
import { nowIso } from '../helpers/now-iso';

const visitsKey = 'vulkan-board:visits';

// the log is bounded: at the cap the oldest page views fall off the front,
// so a scope not visited within the last 200 page views reads as unread
// again. ~60 bytes per entry keeps the stored key under ~12KB.
const maxVisits = 200;

export type PageVisit = {
	href: string;
	visitedAt: string;
};

export class ReadTracking {
	visits: PageVisit[] = $state([]);

	constructor() {
		this.visits = readStoredVisits();
	}

	// the stamp at the end of the log; capture it BEFORE recording the
	// current page so it names the previous visit
	lastVisitDate(): string | null {
		const last = this.visits.at(-1);
		return last === undefined ? null : last.visitedAt;
	}

	// append-only means the log is chronological, so the last matching
	// entry is the scope's newest visit. Dates compare lexicographically:
	// change dates are YYYY-MM-DD, visit stamps full ISO timestamps.
	isUpdated(scopeHrefs: string[], lastChangedDate: string): boolean {
		for (let index = this.visits.length - 1; index >= 0; index--) {
			const visit = this.visits[index];
			if (visit !== undefined && scopeHrefs.includes(visit.href)) {
				return lastChangedDate > visit.visitedAt;
			}
		}
		return true;
	}

	recordPageVisit(href: string): void {
		const visit: PageVisit = { href, visitedAt: nowIso() };
		this.visits = [...this.visits, visit].slice(-maxVisits);
		writeStored(visitsKey, JSON.stringify(this.visits));
	}
}

export const readTracking = new ReadTracking();

// storage access stays wrapped: on the server (no window; Node's own
// localStorage global must never be touched), in a denied or private
// window, absence falls back to the all-new defaults
function readStoredVisits(): PageVisit[] {
	if (typeof window === 'undefined') return [];

	try {
		const raw = window.localStorage.getItem(visitsKey);
		if (raw === null) return [];

		const parsed: unknown = JSON.parse(raw);
		if (!Array.isArray(parsed)) return [];
		const visits: PageVisit[] = [];
		for (const entry of parsed) {
			if (
				typeof entry === 'object' &&
				entry !== null &&
				'href' in entry &&
				typeof entry.href === 'string' &&
				'visitedAt' in entry &&
				typeof entry.visitedAt === 'string'
			) {
				visits.push({ href: entry.href, visitedAt: entry.visitedAt });
			}
		}
		// a stored log larger than the cap trims from the front on load
		return visits.slice(-maxVisits);
	} catch {
		return [];
	}
}

function writeStored(key: string, value: string): void {
	if (typeof window === 'undefined') return;
	try {
		window.localStorage.setItem(key, value);
	} catch {
		// storage denied -- tracking simply stays off for this visit
	}
}
