<script lang="ts">
	import { renderBlock, renderInline } from '#lib/markdown.ts';

	let { src, inline = false }: { src: string; inline?: boolean } = $props();
	// Output is sanitized by DOMPurify in renderInline/renderBlock.
	const html = $derived(inline ? renderInline(src) : renderBlock(src));
</script>

{#if inline}
	<span class="md-inline">{@html html}</span>
{:else}
	<div class="md">{@html html}</div>
{/if}

<style>
	.md-inline :global(a) {
		text-decoration: underline;
		text-decoration-color: var(--border-strong);
	}
	.md :global(p) {
		margin: 0 0 0.6em;
	}
	.md :global(h1),
	.md :global(h2),
	.md :global(h3) {
		font-family: var(--font-display);
		font-weight: 600;
		margin: 0.8em 0 0.4em;
		line-height: 1.2;
	}
	.md :global(h1) {
		font-size: 1.5em;
	}
	.md :global(h2) {
		font-size: 1.25em;
	}
	.md :global(ul),
	.md :global(ol) {
		padding-left: 1.4em;
		margin: 0 0 0.6em;
	}
	.md :global(.task-item) {
		list-style: none;
		margin-left: -1.2em;
	}
	.md :global(.task-item.done) {
		color: var(--muted);
		text-decoration: line-through;
	}
	.md :global(pre) {
		padding: 10px 12px;
		border-radius: var(--r-sm);
		background: var(--chip);
		overflow-x: auto;
	}
	.md :global(pre code) {
		padding: 0;
		background: none;
	}
	.md :global(blockquote) {
		margin: 0 0 0.6em;
		padding-left: 12px;
		border-left: 3px solid var(--border-strong);
		color: var(--muted);
	}
	.md :global(table) {
		border-collapse: collapse;
		margin-bottom: 0.6em;
	}
	.md :global(th),
	.md :global(td) {
		border: 1px solid var(--border);
		padding: 4px 8px;
	}
</style>
