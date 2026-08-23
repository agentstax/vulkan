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
		<a href="/whats-new/">Show what's new since then</a>
	{/if}
</div>

<style src="./visit-bar.css"></style>
