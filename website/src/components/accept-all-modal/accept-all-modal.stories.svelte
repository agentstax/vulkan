<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import AcceptAllModal from './accept-all-modal.svelte';

	const { Story } = defineMeta({
		title: 'Board/AcceptAllModal',
		component: AcceptAllModal,
		args: {
			onDismiss: () => {},
		},
	});
</script>

<Story name="Accept all" />
