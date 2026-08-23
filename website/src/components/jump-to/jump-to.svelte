<script lang="ts">
	import type { JumpTarget } from './types';

	type Props = {
		targets: JumpTarget[];
	};

	let { targets }: Props = $props();

	let selectedHref = $state('');

	function go(): void {
		if (selectedHref === '') {
			return;
		}
		window.location.assign(selectedHref);
	}
</script>

<div class="jump-to">
	<label>
		Jump to:
		<select bind:value={selectedHref}>
			<option value="">Select a board</option>
			{#each targets as target (target.href)}
				<option value={target.href}>{target.label}</option>
			{/each}
		</select>
	</label>
	<button type="button" class="era-button" onclick={go}>Go</button>
</div>

<style src="./jump-to.css"></style>
