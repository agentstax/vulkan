<script lang="ts">
	type Props = {
		width: number;
	};

	let { width }: Props = $props();

	const height = $derived(Math.round(width * (14 / 16)));
</script>

<svg {width} {height} viewBox="0 0 16 14" aria-hidden="true">
	<rect class="body" x="3" y="1" width="1" height="12" />
	<rect class="body" x="4" y="2" width="10" height="7" />
	<rect class="face" x="5" y="3" width="8" height="5" />
	<rect class="highlight" x="5" y="3" width="8" height="1" />
	<rect class="shade" x="5" y="7" width="8" height="1" />
</svg>

<style src="./pixel-flag.css"></style>
