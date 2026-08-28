<script lang="ts">
	type Props = {
		// the phrase the personal text repeats without ever completing
		personalText: string;
	};

	let { personalText }: Props = $props();

	// each copy must be wider than any viewport, so the loop never shows a gap
	const repeated = `${personalText} `.repeat(20);
</script>

<div class="personal-text">
	<span class="personal-text-label">Personal text</span>
	<div class="personal-text-window">
		<span class="personal-text-copy">{repeated}</span>
		<span class="personal-text-copy" aria-hidden="true">{repeated}</span>
	</div>
</div>

<style src="./member-personal-text.css"></style>
