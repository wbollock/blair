<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import { api, ok } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import type { Task } from '#lib/api.ts';

	let { task }: { task: Task } = $props();
	let filter = $state('');
	const shown = $derived(app.labels.filter((l) => l.name.toLowerCase().includes(filter.toLowerCase())));
	const exact = $derived(app.labels.some((l) => l.name.toLowerCase() === filter.trim().toLowerCase()));

	function toggle(id: string) {
		const has = task.labels.includes(id);
		app.update(task.id, { labels: has ? task.labels.filter((x) => x !== id) : [...task.labels, id] });
	}
	async function create() {
		const name = filter.trim().replace(/\s+/g, '-');
		if (!name) return;
		try {
			const l = await ok(api.POST('/labels', { body: { name } }));
			await app.loadLabels();
			app.update(task.id, { labels: [...task.labels, l.id] });
			filter = '';
		} catch (e) {
			app.toast(e instanceof Error ? e.message : 'Could not create label', 'error');
		}
	}
</script>

<div class="filter">
	<input class="input" placeholder="Filter or create…" bind:value={filter} onkeydown={(e) => {
		e.stopPropagation();
		if (e.key === 'Enter' && filter.trim() && !exact) create();
	}} aria-label="Filter labels" />
</div>
{#each shown as l (l.id)}
	<DropdownMenu.CheckboxItem class="menu-item" checked={task.labels.includes(l.id)} closeOnSelect={false} onCheckedChange={() => toggle(l.id)}>
		{#snippet children({ checked })}
			<span class="box" class:on={checked}>{checked ? '✓' : ''}</span>@{l.name}
		{/snippet}
	</DropdownMenu.CheckboxItem>
{/each}
{#if filter.trim() && !exact}
	<DropdownMenu.Item class="menu-item" closeOnSelect={false} onSelect={create}>Create “@{filter.trim().replace(/\s+/g, '-')}”</DropdownMenu.Item>
{/if}
{#if !app.labels.length && !filter}
	<div class="menu-heading">No labels yet: type to create one</div>
{/if}

<style>
	.filter {
		padding: 4px 4px 6px;
	}
	.filter .input {
		min-height: 32px;
		font-size: 13px;
	}
	.box {
		width: 16px;
		height: 16px;
		display: grid;
		place-items: center;
		border-radius: 4px;
		border: 1.5px solid var(--border-strong);
		font-size: 11px;
	}
	.box.on {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
</style>
