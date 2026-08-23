<script lang="ts">
	type Props = {
		title: string;
		// null = a thread with no VK code
		code: string | null;
		solved: boolean;
		editHref: string;
	};

	let { title, code, solved, editHref }: Props = $props();
</script>

<div class="thread-title-band">
	{#if solved}
		<span class="solved-chip">[SOLVED]</span>
	{/if}
	<h1 class="thread-title">
		{title}
		{#if code !== null}<span class="thread-code">[{code}]</span>{/if}
	</h1>
	<a class="era-button" href={editHref}>Edit this page</a>
</div>

<style src="./thread-title-band.css"></style>
