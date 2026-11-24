<script lang="ts">
	import type { Snippet } from 'svelte';
	let { title, subtitle = '', children }: { title: string; subtitle?: string; children: Snippet } = $props();
</script>

<div class="wrap">
	<div class="card">
		<div class="brand">Blair</div>
		<h1>{title}</h1>
		{#if subtitle}<p class="sub">{subtitle}</p>{/if}
		{@render children()}
	</div>
</div>

<style>
	.wrap {
		min-height: 100dvh;
		display: grid;
		place-items: center;
		padding: 24px 16px;
	}
	.card {
		width: min(400px, 100%);
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.brand {
		font-family: var(--font-display);
		font-size: 28px;
		font-weight: 600;
		color: var(--accent-ink);
	}
	h1 {
		margin: 0;
		font-family: var(--font-display);
		font-weight: 600;
		font-size: 26px;
	}
	.sub {
		margin: -6px 0 6px;
		color: var(--muted);
	}
	.card :global(form) {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.card :global(.error) {
		padding: 8px 12px;
		border-radius: var(--r-sm);
		background: var(--p4-soft);
		color: var(--text);
		font-size: 13px;
	}
</style>
