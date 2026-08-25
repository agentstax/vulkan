<script lang="ts">
	import ChromeButton from '../chrome-button/chrome-button.svelte';

	type Props = {
		topic: string;
		text: string;
		errorMessage: string | null;
		disabled: boolean;
		ontext: (next: string) => void;
		onproduce: () => void;
	};

	let { topic, text, errorMessage, disabled, ontext, onproduce }: Props = $props();

	const fieldId = $props.id();
</script>

<div class="produce-message">
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
	<ChromeButton
		label="Produce ▸"
		ariaLabel="Produce a message"
		tone="primary"
		pressed={null}
		{disabled}
		onclick={onproduce}
	/>
</div>
{#if errorMessage !== null}
	<div class="produce-error" role="alert">{errorMessage}</div>
{/if}

<style src="./produce-message.css"></style>
