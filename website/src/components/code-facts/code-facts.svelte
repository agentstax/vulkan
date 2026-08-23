<script lang="ts">
	type Props = {
		code: string;
		classification: string;
		consequence: string;
	};

	let { code, classification, consequence }: Props = $props();
</script>

<div class="code-facts">
	code {code} &middot; <span class="classification">{classification}</span> — {consequence}
</div>

<style src="./code-facts.css"></style>
