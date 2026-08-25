<script lang="ts">
	import { sqlSegments } from './highlight';

	type Props = {
		sql: string;
	};

	let { sql }: Props = $props();

	const segments = $derived(sqlSegments(sql));
</script>

<!-- whitespace inside pre is content, so tags stay glued and the
     line break hides inside the span's opening tag -->
<pre class="sql">{#each segments as segment, index (index)}<span
			class="sql-segment"
			data-kind={segment.kind}>{segment.text}</span
		>{/each}</pre>

<style src="./highlighted-sql.css"></style>
