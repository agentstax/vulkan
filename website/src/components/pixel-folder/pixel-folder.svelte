<script lang="ts">
	type Props = {
		updated: boolean;
		pinned: boolean;
		width: number;
	};

	let { updated, pinned, width }: Props = $props();

	// the pin badge widens the canvas by one cell and adds a row above
	const viewBox = $derived(pinned ? '0 0 17 15' : '0 0 16 14');
	const height = $derived(pinned ? Math.round(width * (15 / 17)) : Math.round(width * (14 / 16)));
</script>

<svg {width} {height} {viewBox} data-updated={updated} aria-hidden="true">
	<g transform={pinned ? 'translate(0 1)' : null}>
		<rect class="body" x="1" y="2" width="6" height="2" />
		<rect class="face" x="2" y="3" width="4" height="1" />
		<rect class="body" x="1" y="4" width="14" height="9" />
		<rect class="face" x="2" y="5" width="12" height="7" />
		<rect class="highlight" x="2" y="5" width="12" height="1" />
		<rect class="shade" x="2" y="11" width="12" height="1" />
	</g>
	{#if pinned}
		<rect class="pin" x="12" y="0" width="4" height="4" />
		<rect class="pin-light" x="12" y="0" width="2" height="1" />
		<rect class="pin-dark" x="13" y="4" width="2" height="2" />
	{/if}
</svg>

<style src="./pixel-folder.css"></style>
