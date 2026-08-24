<script lang="ts">
	import type { ChromeButtonTone } from './types';

	type Props = {
		label: string;
		tone: ChromeButtonTone;
		disabled: boolean;
		onclick: () => void;
	};

	let { label, tone, disabled, onclick }: Props = $props();
</script>

<button type="button" class="chrome-button" data-tone={tone} {disabled} {onclick}>{label}</button>

<style src="./chrome-button.css"></style>
