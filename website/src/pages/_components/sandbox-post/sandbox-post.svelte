<script lang="ts">
	import Sandbox from '../../../components/sandbox/sandbox.svelte';
	import ThreadPost from '../../../components/thread-post/thread-post.svelte';
	import type { PanelShell } from '../../../components/sandbox/types';
	import ExampleQuery from '../example-query/example-query.svelte';

	type Props = {
		topic: string;
		messages: PanelShell;
		cursors: PanelShell;
	};

	let { topic, messages, cursors }: Props = $props();
</script>

<div class="sandbox-post">
	<ThreadPost author="brandon" role="Site Admin" header={null} postCount={null} actions={null}>
		<h3 class="headline">It's Kafka on Postgres.</h3>
		<p>You can do cool things like this</p>
		<div class="sandbox-region">
			<Sandbox {topic} {messages} {cursors} />
		</div>
		<div class="example-region">
			<ExampleQuery panel={messages} />
		</div>
	</ThreadPost>
</div>

<style src="./sandbox-post.css"></style>
