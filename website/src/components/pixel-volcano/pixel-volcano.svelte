<script lang="ts">
	type Props = {
		width: number;
	};

	let { width }: Props = $props();
</script>

<svg {width} height={width} viewBox="0 0 16 16" aria-hidden="true">
	<rect class="lava-bright" x="7" y="2" width="2" height="1" />
	<rect class="lava-mid" x="6" y="3" width="4" height="1" />
	<rect class="lava-deep" x="5" y="4" width="6" height="1" />
	<rect class="rock-1" x="6" y="5" width="4" height="1" />
	<rect class="rock-2" x="5" y="6" width="6" height="2" />
	<rect class="rock-3" x="4" y="8" width="8" height="2" />
	<rect class="rock-4" x="3" y="10" width="10" height="2" />
	<rect class="rock-5" x="2" y="12" width="12" height="2" />
	<rect class="rock-6" x="1" y="14" width="14" height="1" />
	<rect class="lava-mid" x="5" y="6" width="1" height="2" />
	<rect class="lava-deep" x="4" y="8" width="1" height="2" />
	<rect class="lava-deep" x="3" y="10" width="1" height="2" />
	<rect class="lava-mid" x="10" y="6" width="1" height="1" />
	<rect class="lava-deep" x="10" y="7" width="1" height="1" />
	<rect class="lava-deep" x="11" y="8" width="1" height="2" />
</svg>

<style src="./pixel-volcano.css"></style>
