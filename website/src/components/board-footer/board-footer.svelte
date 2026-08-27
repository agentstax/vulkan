<script lang="ts">
	import { repositoryUrl } from '../../site';
	import { boardStyle, boardStyles } from '../../state/board-style.svelte';
</script>

<div class="board-footer">
	<div class="footer-links">
		<a href="{repositoryUrl}/graphs/contributors">The team</a>
	</div>
	<div class="footer-meta">
		<span>All times are UTC</span>
		<label class="board-style">
			Board style:
			<select bind:value={() => boardStyle.current, (id) => boardStyle.select(id)}>
				{#each boardStyles as style (style.id)}
					<option value={style.id}>{style.label}</option>
				{/each}
			</select>
		</label>
	</div>
</div>

<style src="./board-footer.css"></style>
