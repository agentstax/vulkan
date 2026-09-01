<script lang="ts">
	import PixelVolcano from '../../../../components/pixel-volcano/pixel-volcano.svelte';
	import MemberPersonalText from '../member-personal-text/member-personal-text.svelte';

	type Props = {
		author: string;
		role: string;
		joinedDate: string;
		postCount: number;
		websiteHref: string;
		personalText: string;
	};

	let { author, role, joinedDate, postCount, websiteHref, personalText }: Props = $props();
</script>

<div class="member-profile">
	<div class="profile-columns">
		<div class="identity">
			<span class="member-name">{author}</span>
			<span class="stars">
				{#each [0, 1, 2] as star (star)}
					<svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
						<path
							d="M5 0 L6.3 3.4 L10 3.6 L7.1 5.9 L8.1 9.5 L5 7.4 L1.9 9.5 L2.9 5.9 L0 3.6 L3.7 3.4 z"
						/>
					</svg>
				{/each}
			</span>
			<span class="member-role">{role}</span>
			<span class="avatar"><PixelVolcano width={88} /></span>
		</div>
		<dl class="facts">
			<dt>Joined</dt>
			<dd>{joinedDate}</dd>
			<dt>Total posts</dt>
			<dd>{postCount}</dd>
			<dt>Website</dt>
			<dd><a href={websiteHref}>{websiteHref}</a></dd>
		</dl>
	</div>
	<!-- little stanley reference for the cultured folks out there -->
	<MemberPersonalText {personalText} />
</div>

<style src="./member-profile.css"></style>
