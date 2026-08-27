<script lang="ts" module>
	import { defineMeta } from '@storybook/addon-svelte-csf';
	import { siteNotice } from '../../state/site-notice.svelte';
	import SiteNotice from './site-notice.svelte';

	const { Story } = defineMeta({
		title: 'Board/SiteNotice',
		component: SiteNotice,
	});

	// the component reads the one page-level notice, so each story sets it
	// after mounting; clear first, or a lower face is refused
	function set(kind: 'banner' | 'modal', detail: string | null): () => void {
		return () => {
			siteNotice.clear();
			siteNotice.show(kind, detail);
		};
	}
</script>

<Story name="Nothing to report" play={() => siteNotice.clear()} />
<Story name="Banner" play={set('banner', 'relation "delivery" does not exist')} />
<Story name="Banner without detail" play={set('banner', null)} />
<Story name="Reload modal" play={set('modal', null)} />
