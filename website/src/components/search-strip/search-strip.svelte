<script lang="ts">
	type Props = {
		exampleText: string;
	};

	let { exampleText }: Props = $props();
</script>

<div class="search-strip">
	<span class="prompt">
		Landed here from a log? Paste any Vulkan error line into Search — every code has a thread.
	</span>
	<span class="example">{exampleText}</span>
	<!-- inert until the Pagefind search island exists -->
	<button type="button" class="era-button">Search</button>
</div>

<style src="./search-strip.css"></style>
