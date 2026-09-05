<script lang="ts">
	type Props = {
		text: string;
		kind: 'error' | 'event' | 'metric' | 'alert';
	};

	let { text, kind }: Props = $props();
</script>

<pre class="log-line" data-kind={kind}>{text}</pre>

<style src="./log-line.css"></style>
