<script lang="ts">
	type Props = {
		lastVisitDate: string | null;
	};

	let { lastVisitDate }: Props = $props();
</script>

<div class="visit-bar">
	{#if lastVisitDate === null}
		<span>Welcome — this is your first visit</span>
	{:else}
		<span class="visit-date">You last visited on {lastVisitDate}</span>
		<button type="button">Show what's new since then</button>
	{/if}
</div>

<style src="./visit-bar.css"></style>
