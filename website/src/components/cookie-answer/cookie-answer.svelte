<script lang="ts">
	import type { Answer } from './types';

	type Props = {
		answer: Answer;
		titleId: string;
		onDismiss: () => void;
	};

	let { answer, titleId, onDismiss }: Props = $props();
</script>

<p class="answer-body" id={titleId}>{answer.body}</p>
<div class="answer-actions">
	{#if answer.acceptLabel && answer.acceptLink}
		<a class="era-button" target="_blank" rel="noreferrer" href={answer.acceptLink}>
			<span>{answer.acceptLabel}</span>
		</a>
	{/if}
	<button type="button" class="era-button" onclick={onDismiss}>
		<span>{answer.dismissLabel}</span>
	</button>
</div>
<p class="answer-statement">
	But for real we ain't got no cookies here. I don't want your data. You're probably a broke boy
	anyway.
</p>

<style src="./cookie-answer.css"></style>
