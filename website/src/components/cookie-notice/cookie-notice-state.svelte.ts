import { nowIso } from '../../helpers/now-iso';
import type { ConsentButton } from './answers';

// The site sets no cookies, so this notice is the privacy statement wearing
// a consent banner's clothes: act one asks what every site asks, act two
// answers it. The stored answer is the only thing the notice itself keeps.
const answeredKey = 'vulkan-board:cookie-notice';

// consent: the banner as any site would show it
// answered: the reader pressed one of act one's controls and is reading
//   what that control had to say
// closed: this browser has answered and the notice is done
export type CookieNoticeAct = 'consent' | 'answered' | 'closed';

export class CookieNoticeState {
	act: CookieNoticeAct = $state('closed');

	// which control act one was answered with; it picks the face the answer
	// arrives on and the words on it
	answered: ConsentButton = $state('accept');

	// Called from the component's onMount. The module singleton survives
	// ClientRouter swaps, so a reader mid-notice keeps their act across a
	// navigation and a reader who already answered never opens it again.
	open(): void {
		if (this.act !== 'closed' || hasAnswered()) return;
		this.act = 'consent';
	}

	// stored at the press rather than at dismiss, so closing the tab while
	// still reading the answer counts as answered
	recordAnswer(button: ConsentButton): void {
		this.answered = button;
		this.act = 'answered';
		markAnswered();
	}

	close(): void {
		this.act = 'closed';
	}
}

export const cookieNotice = new CookieNoticeState();

// ***************
// *** HELPERS ***
// ***************

// storage access stays wrapped: on the server there is nothing to read, and
// a denied or private window simply sees the notice again next visit
function hasAnswered(): boolean {
	if (typeof window === 'undefined') return true;

	try {
		return window.localStorage.getItem(answeredKey) !== null;
	} catch {
		return false;
	}
}

function markAnswered(): void {
	if (typeof window === 'undefined') return;

	try {
		window.localStorage.setItem(answeredKey, nowIso());
	} catch {
		// storage denied -- the notice returns on the next visit
	}
}
