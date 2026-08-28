<script lang="ts">
	import Sandbox from '../../../components/sandbox/sandbox.svelte';
	import ThreadPost from '../../../components/thread-post/thread-post.svelte';
	import type { PanelShell } from '../../../components/sandbox/types';

	type Props = {
		label: string;
		topic: string;
		messages: PanelShell;
		cursors: PanelShell;
	};

	let { label, topic, messages, cursors }: Props = $props();
</script>

<div class="sandbox-post">
	<ThreadPost author="brandon" role="Site Admin" header={null} postCount={null} actions={null}>
		<p>
			Every queue makes you trust its dashboard. Vulkan is Postgres, so you can just look — the
			tables below are the ones the library creates, seeded through its own produce statement. Edit
			the SQL and press Run: the database runs in this tab, and nothing leaves your browser.
		</p>
		<Sandbox {label} {topic} {messages} {cursors} />
	</ThreadPost>
</div>

<style src="./sandbox-post.css"></style>
