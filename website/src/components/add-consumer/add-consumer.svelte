<script lang="ts">
	import ChromeButton from '../chrome-button/chrome-button.svelte';

	type Props = {
		groups: string[];
		onadd: (group: string | null) => void;
	};

	let { groups, onadd }: Props = $props();

	// null is a group that does not exist yet; a name joins the group that does
	let selected: string | null = $state(null);

	const fieldId = $props.id();
</script>

<div class="add-consumer">
	<label class="add-prompt" for={fieldId}>Add a consumer to</label>
	<select
		class="group-select"
		id={fieldId}
		onchange={(event) => {
			// a select carries strings only, so the empty option is read back as
			// the absent group here rather than travelling as one
			const value = event.currentTarget.value;
			selected = value === '' ? null : value;
		}}
	>
		<option value="">a new group…</option>
		{#each groups as group (group)}
			<option value={group}>group {group}</option>
		{/each}
	</select>
	<ChromeButton label="Add" tone="quiet" disabled={false} onclick={() => onadd(selected)} />
	<span class="add-hint">
		New group = its own cursor, reads everything. Existing group = one cursor, disjoint ranges.
	</span>
</div>

<style src="./add-consumer.css"></style>
