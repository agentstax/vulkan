<script lang="ts">
	import ChromeButton from '../chrome-button/chrome-button.svelte';

	type Props = {
		topic: string;
		text: string;
		ontext: (next: string) => void;
	};

	let { topic, text, ontext }: Props = $props();

	const fieldId = $props.id();
</script>

<div class="produce-strip">
	<label class="produce-label" for={fieldId}>
		Produce to <span class="produce-topic">{topic}</span>
	</label>
	<input
		class="produce-field"
		id={fieldId}
		type="text"
		value={text}
		oninput={(event) => ontext(event.currentTarget.value)}
	/>
	<ChromeButton label="Produce ▸" tone="primary" disabled={false} onclick={() => {}} />
</div>

<style src="./produce-strip.css"></style>
