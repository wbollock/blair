<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import { app } from '#lib/state.svelte.ts';
	import type { Task } from '#lib/api.ts';

	let { task }: { task: Task } = $props();
	const cols = $derived(app.project(task.project_id)?.columns ?? []);
</script>

{#if cols.length === 0}
	<div class="menu-heading">Pick a project first</div>
{/if}
{#each cols as c (c.id)}
	<DropdownMenu.Item class="menu-item" onSelect={() => app.update(task.id, { column_id: c.id }, { undo: `Moved to ${c.name}` })}>
		<span class="status s-{c.status}"></span>
		<span>{c.name}</span>
		<span class="kbd">{task.column_id === c.id ? '✓ ' : ''}{c.status}</span>
	</DropdownMenu.Item>
{/each}

<style>
	.status {
		width: 8px;
		height: 8px;
		border-radius: 99px;
		border: 1.5px solid var(--faint);
	}
	.s-doing,
	.s-waiting {
		background: var(--accent);
		border-color: var(--accent);
	}
	.s-done {
		background: var(--done);
		border-color: var(--done);
	}
	.s-cancelled {
		background: var(--faint);
	}
</style>
