<script lang="ts">
	import type { Snippet } from 'svelte';
	import { caughtMessage } from '../../helpers/caught-message';

	type Props = {
		// the section as a reader would name it: "sandbox", "search"
		name: string;
		children: Snippet;
	};

	let { name, children }: Props = $props();
</script>

<!-- the boundary catches throws from rendering and effects in its children;
     a throw in a DOM handler or async work never reaches it, so those keep
     their own try/catch at the call site -->
<svelte:boundary>
	{@render children()}
	{#snippet failed(error, reset)}
		<div class="island-fallback" role="alert">
			<p class="fallback-problem">the {name} stopped — the rest of the page still works</p>
			<p class="fallback-detail">{caughtMessage(error)}</p>
			<button type="button" class="era-button" onclick={() => reset()}>Try again</button>
		</div>
	{/snippet}
</svelte:boundary>

<style src="./island-boundary.css"></style>
