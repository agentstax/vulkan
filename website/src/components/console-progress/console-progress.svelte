<script lang="ts">
	import type { DatabaseStage } from '../sql-console/database';

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

<style src="./console-progress.css"></style>
