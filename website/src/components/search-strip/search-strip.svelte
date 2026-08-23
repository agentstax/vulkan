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
	<a class="era-button" href="/search/">Search</a>
</div>

<style src="./search-strip.css"></style>
