<script lang="ts">
	import type { ChromeButtonTone } from './types';

	type Props = {
		label: string;
		// what the button is called without its chrome: labels carry glyphs
		// (▸, ↻, ✕) that read as punctuation or nothing at all when spoken
		ariaLabel: string;
		tone: ChromeButtonTone;
		disabled: boolean;
		onclick: () => void;
	};

	let { label, ariaLabel, tone, disabled, onclick }: Props = $props();
</script>

<button
	type="button"
	class="chrome-button"
	data-tone={tone}
	aria-label={ariaLabel}
	{disabled}
	{onclick}>{label}</button
>

<style src="./chrome-button.css"></style>
