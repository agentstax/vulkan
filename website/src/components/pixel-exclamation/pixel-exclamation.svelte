<script lang="ts">
	type Props = {
		width: number;
	};

	let { width }: Props = $props();
</script>

<svg {width} height={width} viewBox="0 0 16 16" aria-hidden="true">
	<rect x="7" y="2" width="2" height="7" class="pin" />
	<rect x="7" y="2" width="1" height="7" class="pin-light" />
	<rect x="7" y="8" width="2" height="1" class="pin-dark" />
	<rect x="7" y="11" width="2" height="2" class="pin" />
	<rect x="7" y="11" width="1" height="1" class="pin-light" />
</svg>

<style src="./pixel-exclamation.css"></style>
