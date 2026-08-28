<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import CookieNotice from './cookie-notice.svelte';
	import { cookieNotice } from './cookie-notice-state.svelte';
	import type { ConsentButton } from './answers';

	const { Story } = defineMeta({
		title: 'Board/CookieNotice',
		component: CookieNotice,
	});

	// the component reads the one notice state and mounting opens it from
	// storage, so each story sets the act it wants afterwards
	function consent(): void {
		cookieNotice.act = 'consent';
	}

	function answered(button: ConsentButton): () => void {
		return () => {
			cookieNotice.answered = button;
			cookieNotice.act = 'answered';
		};
	}

	function closed(): void {
		cookieNotice.close();
	}
</script>

<Story name="Consent" play={consent} />
<Story name="Accept all — the modal" play={answered('accept')} />
<Story name="Reject non-essential" play={answered('reject')} />
<Story name="Manage preferences" play={answered('manage')} />
<Story name="Answered already" play={closed} />
