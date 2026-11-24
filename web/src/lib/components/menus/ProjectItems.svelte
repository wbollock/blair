<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import { app } from '#lib/state.svelte.ts';
	import type { Task } from '#lib/api.ts';

	let { task }: { task: Task } = $props();
	const projects = $derived(app.projects.filter((p) => !p.archived_at));
</script>

<DropdownMenu.Item class="menu-item" onSelect={() => app.update(task.id, { project_id: null }, { undo: 'Moved to Inbox' })}>
	<span class="dot" style="background: var(--faint)"></span>Inbox
	{#if !task.project_id}<span class="kbd">✓</span>{/if}
</DropdownMenu.Item>
{#each projects as p (p.id)}
	<DropdownMenu.Item class="menu-item" onSelect={() => app.update(task.id, { project_id: p.id }, { undo: `Moved to ${p.name}` })}>
		<span class="dot" style="background: {p.color || 'var(--accent)'}"></span>{p.name}
		{#if task.project_id === p.id}<span class="kbd">✓</span>{/if}
	</DropdownMenu.Item>
{/each}
