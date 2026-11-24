<script lang="ts">
	import type { Snippet } from 'svelte';
	import QuickAdd from './QuickAdd.svelte';
	import JustAddedTray from './JustAddedTray.svelte';

	let { children, quickAdd = true, wide = false }: { children: Snippet; quickAdd?: boolean; wide?: boolean } = $props();
</script>

<div class="page" class:wide>
	{#if quickAdd}
		<div class="capture">
			<div class="desktop-only"><QuickAdd /></div>
			<JustAddedTray />
		</div>
	{/if}
	{@render children()}
</div>

<style>
	.page {
		max-width: 860px;
	}
	.page.wide {
		max-width: none;
	}
	.capture {
		display: flex;
		flex-direction: column;
		gap: 14px;
		margin-bottom: 8px;
	}
	@media (max-width: 640px) {
		.desktop-only {
			display: none;
		}
	}
</style>
