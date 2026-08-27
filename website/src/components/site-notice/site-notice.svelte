<script lang="ts">
	import { siteNotice } from '../../state/site-notice.svelte';

	function reload(): void {
		window.location.reload();
	}
</script>

{#if siteNotice.current !== null}
	{#if siteNotice.current.kind === 'banner'}
		<div class="notice-banner" role="alert">
			<p class="notice-problem">part of this page stopped — the rest still works</p>
			{#if siteNotice.current.detail !== null}
				<p class="notice-detail">{siteNotice.current.detail}</p>
			{/if}
			<button type="button" class="era-button" onclick={() => siteNotice.dismiss()}>
				Dismiss
			</button>
		</div>
	{:else}
		<div class="notice-veil">
			<div
				class="notice-box"
				role="alertdialog"
				aria-modal="true"
				aria-labelledby="site-notice-problem"
			>
				<p class="notice-problem" id="site-notice-problem">
					part of this page could not load — a newer version of the site may have replaced it
				</p>
				{#if siteNotice.current.detail !== null}
					<p class="notice-detail">{siteNotice.current.detail}</p>
				{/if}
				<div class="notice-actions">
					<button type="button" class="era-button" onclick={reload}>Reload</button>
					<button type="button" class="era-button" onclick={() => siteNotice.dismiss()}>
						Not now
					</button>
				</div>
			</div>
		</div>
	{/if}
{/if}

<style src="./site-notice.css"></style>
