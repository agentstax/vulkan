import { newLeakedAccount, type LeakedAccount } from './leaked-account';

// One digit at a time, because the dump has to happen in front of the
// reader -- a number already sitting there is a label, a number arriving is
// something being taken.
const digitIntervalMs = 55;

export class LeakedAccountReveal {
	account: LeakedAccount = $state(newLeakedAccount());
	typed: number = $state(0);

	get routingShown(): string {
		return this.account.routingNumber.slice(0, this.typed);
	}

	get accountShown(): string {
		const remaining = this.typed - this.account.routingNumber.length;
		return remaining <= 0 ? '' : this.account.accountNumber.slice(0, remaining);
	}

	get done(): boolean {
		return this.typed >= this.total;
	}

	get total(): number {
		return this.account.routingNumber.length + this.account.accountNumber.length;
	}

	// The timer is a real side effect, so the component owns its lifetime:
	// this returns the stop. A reader who asked for less motion is handed
	// the finished dump rather than a slower one.
	start(): () => void {
		if (prefersReducedMotion()) {
			this.typed = this.total;
			return () => {};
		}

		const timer = window.setInterval(() => {
			this.typed += 1;
			if (this.done) window.clearInterval(timer);
		}, digitIntervalMs);

		return () => window.clearInterval(timer);
	}
}

// ***************
// *** HELPERS ***
// ***************

function prefersReducedMotion(): boolean {
	if (typeof window === 'undefined') return true;
	return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}
