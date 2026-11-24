<script lang="ts">
	import type { Snippet } from 'svelte';
	let { title, subtitle = '', actions }: { title: string; subtitle?: string; actions?: Snippet } = $props();
</script>

<header class="vh">
	<div>
		<h1>{title}</h1>
		{#if subtitle}<p>{subtitle}</p>{/if}
	</div>
	{#if actions}<div class="actions">{@render actions()}</div>{/if}
</header>

<style>
	.vh {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: 16px;
		flex-wrap: wrap;
		margin-bottom: 18px;
		padding-right: 44px;
	}
	h1 {
		margin: 0;
		font-family: var(--font-display);
		font-size: 34px;
		font-weight: 600;
		letter-spacing: -0.02em;
		line-height: 1.1;
	}
	p {
		margin: 4px 0 0;
		color: var(--muted);
		font-size: 13px;
	}
	.actions {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	@media (min-width: 1024px) {
		.vh {
			padding-right: 0;
		}
	}
	@media (max-width: 640px) {
		h1 {
			font-size: 30px;
		}
	}
</style>
