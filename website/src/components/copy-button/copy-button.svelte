<script lang="ts">
	import { CopyButtonState } from './copy-button-state.svelte';

	type Props = {
		label: string;
		text: string;
	};

	let { label, text }: Props = $props();

	const state = new CopyButtonState();

	const display = $derived(
		state.phase === 'copied' ? 'Copied' : state.phase === 'failed' ? 'could not copy' : label,
	);
</script>

<button class="copy-button" data-phase={state.phase} onclick={() => void state.copy(text)}>
	{display}
</button>

<style src="./copy-button.css"></style>
