<script lang="ts">
	import type { ChromeButtonTone } from './types';

	type Props = {
		label: string;
		// what the button is called without its chrome: labels carry glyphs
		// (▸, ↻, ✕) that read as punctuation or nothing at all when spoken
		ariaLabel: string;
		tone: ChromeButtonTone;
		// a toggle's on/off state, null for a button that does its action once.
		// A button that is not a toggle must carry no aria-pressed at all, which
		// is what null renders as
		pressed: boolean | null;
		disabled: boolean;
		onclick: () => void;
	};

	let { label, ariaLabel, tone, pressed, disabled, onclick }: Props = $props();
</script>

<button
	type="button"
	class="chrome-button"
	data-tone={tone}
	aria-label={ariaLabel}
	aria-pressed={pressed}
	{disabled}
	{onclick}>{label}</button
>

<style src="./chrome-button.css"></style>
