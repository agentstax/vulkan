<script lang="ts">
	import { onMount } from 'svelte';
	import { repositoryUrl } from '../../site';
	import { LeakedAccountReveal } from './accept-all-modal-state.svelte';
	import { memes } from './memes';

	type Props = {
		onDismiss: () => void;
	};

	let { onDismiss }: Props = $props();

	const titleId = 'accept-all-body';

	const reveal = new LeakedAccountReveal();

	onMount(() => reveal.start());

	// the veil covers the page, so Escape has to be a way out of it
	function dismissOnEscape(event: KeyboardEvent): void {
		if (event.key === 'Escape') onDismiss();
	}
</script>

<svelte:window onkeydown={dismissOnEscape} />

<div class="modal-veil">
	{#each memes as meme (meme.left + meme.top)}
		<img
			class="modal-meme"
			style="top: {meme.top}; left: {meme.left}; width: {meme.width}px; rotate: {meme.tilt}deg"
			data-framed={meme.image.framed}
			src={meme.image.source}
			width={meme.image.naturalWidth}
			height={meme.image.naturalHeight}
			alt=""
		/>
	{/each}

	<div class="modal-box" role="dialog" aria-modal="true" aria-labelledby={titleId}>
		<p class="modal-body" id={titleId}>
			Ah shit sorry bro, you've been hacked. Shouldn't have accepted all.
		</p>
		<dl class="leaked">
			<div class="leaked-row">
				<dt class="leaked-label">Routing number</dt>
				<dd class="leaked-value">{reveal.routingShown}</dd>
			</div>
			<div class="leaked-row">
				<dt class="leaked-label">Account number</dt>
				<dd class="leaked-value">{reveal.accountShown}</dd>
			</div>
		</dl>
		<div class="modal-actions">
			<a class="era-button" target="_blank" rel="noreferrer" href={repositoryUrl}>
				please no, I'll do anything
			</a>
			<button type="button" class="era-button" onclick={onDismiss}>accept fate</button>
		</div>
	</div>
	<!-- <p class="modal-statement">
			But for real we ain't got no cookies here. I don't want your data. You're probably a broke boy
			anyway.
	</p> these scum don't deserve this -->
</div>

<style src="./accept-all-modal.css"></style>
