<script lang="ts">
	import { app } from '#lib/state.svelte.ts';
	import { dueLabel } from '#lib/dates.ts';
	import type { Task } from '#lib/api.ts';

	let { task, showProject = true }: { task: Task; showProject?: boolean } = $props();
	const project = $derived(app.project(task.project_id));
	const column = $derived(app.column(task.column_id));
	const statusMismatch = $derived(column && column.status !== task.status);
</script>

<span class="meta">
	{#if task.checklist_total}
		<span class="m" title="Checklist">☑ {task.checklist_done}/{task.checklist_total}</span>
	{/if}
	{#if task.description}
		<span class="m" aria-label="Has notes" title="Has notes">≡</span>
	{/if}
	{#if statusMismatch}
		<span class="badge">{task.status}</span>
	{/if}
	{#each task.labels as id (id)}
		{@const l = app.label(id)}
		{#if l}<span class="m">@{l.name}</span>{/if}
	{/each}
	{#if showProject && project}
		<span class="m proj"><span class="dot" style="background: {project.color || 'var(--accent)'}"></span>{project.name}{column ? ` · ${column.name}` : ''}</span>
	{/if}
	{#if task.due_date}
		<span class="m due" class:overdue={task.is_overdue}>{dueLabel(task.due_date, task.due_time, app.tz)}</span>
	{/if}
</span>

<style>
	.meta {
		display: inline-flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
		font-size: 12px;
		color: var(--muted);
	}
	.m {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		white-space: nowrap;
	}
	.proj .dot {
		width: 7px;
		height: 7px;
	}
	.due.overdue {
		color: var(--p4);
		font-weight: 600;
	}
	.badge {
		padding: 0 6px;
		border-radius: 4px;
		background: var(--chip);
		font-size: 11px;
	}
</style>
