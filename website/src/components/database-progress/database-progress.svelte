<script lang="ts">
	import type { DatabaseStage } from '../sandbox/database';

	type Props = {
		stage: DatabaseStage;
	};

	let { stage }: Props = $props();
</script>

<div class="progress" role="status">
	<span class="progress-title">Connecting to database</span>
	<span class="progress-stage">{stage}…</span>
	<div class="progress-track">
		<div class="progress-fill"></div>
	</div>
</div>

<style src="./database-progress.css"></style>
