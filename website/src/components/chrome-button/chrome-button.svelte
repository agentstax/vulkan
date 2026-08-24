<script lang="ts">
	type Props = {
		label: string;
		disabled: boolean;
		onclick: () => void;
	};

	let { label, disabled, onclick }: Props = $props();
</script>

<button type="button" class="chrome-button" {disabled} {onclick}>{label}</button>

<style src="./chrome-button.css"></style>
