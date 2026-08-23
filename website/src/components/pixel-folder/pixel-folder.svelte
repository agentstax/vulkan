<script lang="ts">
	type Props = {
		updated?: boolean;
		width?: number;
	};

	let { updated = false, width = 18 }: Props = $props();

	const height = $derived(Math.round(width * (14 / 16)));
</script>

<svg {width} {height} viewBox="0 0 16 14" data-updated={updated} aria-hidden="true">
	<rect class="body" x="1" y="2" width="6" height="2" />
	<rect class="face" x="2" y="3" width="4" height="1" />
	<rect class="body" x="1" y="4" width="14" height="9" />
	<rect class="face" x="2" y="5" width="12" height="7" />
	<rect class="highlight" x="2" y="5" width="12" height="1" />
	<rect class="shade" x="2" y="11" width="12" height="1" />
</svg>

<style src="./pixel-folder.css"></style>
