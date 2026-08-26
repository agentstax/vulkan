<script lang="ts">
	import { fillSegments, sqlSegments } from './highlight';

	type Props = {
		sql: string;
		// the reader's own values, keyed by attribute name
		// an empty map doesn't fill any placeholders
		values: Map<string, string>;
	};

	let { sql, values }: Props = $props();

	const segments = $derived(fillSegments(sqlSegments(sql), values));
</script>

<!-- whitespace inside pre is content, so tags stay glued and the
     line break hides inside the span's opening tag -->
<pre class="sql">{#each segments as segment, index (index)}<span
			class="sql-segment"
			data-kind={segment.kind}>{segment.text}</span
		>{/each}</pre>

<style src="./highlighted-sql.css"></style>
