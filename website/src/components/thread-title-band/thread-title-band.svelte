<script lang="ts">
	type Props = {
		title: string;
		editHref: string;
	};

	let { title, editHref }: Props = $props();
</script>

<div class="thread-title-band">
	<h1 class="thread-title">{title}</h1>
	<a class="era-button" href={editHref}>Edit this page</a>
</div>

<style src="./thread-title-band.css"></style>
