<script lang="ts">
	import { onMount } from 'svelte';
	import AcceptAllModal from '../accept-all-modal/accept-all-modal.svelte';
	import CookieAnswer from '../cookie-answer/cookie-answer.svelte';
	import { runPageStutter } from '../../helpers/page-stutter';
	import { answers } from './answers';
	import { cookieNotice } from './cookie-notice-state.svelte';

	const titleId = 'cookie-notice-title';

	const answer = $derived(answers[cookieNotice.answered]);

	// the banner is still sitting there while the page flinches, so the
	// stutter reads as something arriving rather than as the click landing
	let accepting = $state(false);

	onMount(() => {
		cookieNotice.open();
	});

	async function acceptAll(): Promise<void> {
		if (accepting) return;

		accepting = true;
		try {
			await runPageStutter();
			cookieNotice.recordAnswer('accept');
		} finally {
			accepting = false;
		}
	}
</script>

{#if cookieNotice.act === 'consent'}
	<div class="cookie-notice" role="dialog" aria-labelledby={titleId}>
		<p class="notice-title" id={titleId}>We value your privacy</p>
		<p class="notice-body">
			We use cookies and similar technologies to remember your preferences, measure site
			performance, and personalize the content you see. Select "Accept all" to consent, or "Reject
			non-essential" to allow only what the site needs to work.
		</p>
		<div class="notice-actions">
			<button type="button" class="era-button" onclick={() => void acceptAll()}>
				Accept all
			</button>
			<button type="button" class="era-button" onclick={() => cookieNotice.recordAnswer('reject')}>
				Reject non-essential
			</button>
			<button
				type="button"
				class="notice-policy"
				onclick={() => cookieNotice.recordAnswer('manage')}
			>
				Manage preferences
			</button>
		</div>
	</div>
{:else if cookieNotice.act === 'answered'}
	{#if answer.face === 'modal'}
		<AcceptAllModal onDismiss={() => cookieNotice.close()} />
	{:else}
		<div class="cookie-notice" role="dialog" aria-labelledby={titleId}>
			<CookieAnswer answer={answer.content} {titleId} onDismiss={() => cookieNotice.close()} />
		</div>
	{/if}
{/if}

<style src="./cookie-notice.css"></style>
