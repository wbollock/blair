<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import { app } from '#lib/state.svelte.ts';
	import { addDays, nextWeek, thisWeekend } from '#lib/dates.ts';
	import type { Task } from '#lib/api.ts';

	let { task }: { task: Task } = $props();
	const options = $derived([
		{ label: 'Today', date: app.today },
		{ label: 'Tomorrow', date: addDays(app.today, 1) },
		{ label: 'This weekend', date: thisWeekend(app.tz) },
		{ label: 'Next week', date: nextWeek(app.tz, app.me?.week_start ?? 'monday') }
	]);
</script>

{#each options as o (o.label)}
	<DropdownMenu.Item class="menu-item" onSelect={() => app.update(task.id, { due_date: o.date }, { undo: `Due ${o.label.toLowerCase()}` })}>
		{o.label}<span class="kbd">{task.due_date === o.date ? '✓' : o.date.slice(5)}</span>
	</DropdownMenu.Item>
{/each}
{#if task.due_date}
	{@const later = addDays(task.due_date > app.today ? task.due_date : app.today, 7)}
	<DropdownMenu.Item class="menu-item" onSelect={() => app.update(task.id, { due_date: later }, { undo: 'Postponed a week' })}>
		Postpone a week<span class="kbd">{later.slice(5)}</span>
	</DropdownMenu.Item>
{/if}
<DropdownMenu.Item class="menu-item" onSelect={() => (app.drawer = task.id)}>Pick a date…</DropdownMenu.Item>
{#if task.due_date}
	<DropdownMenu.Item class="menu-item" onSelect={() => app.update(task.id, { due_date: null }, { undo: 'Due date removed' })}>No date</DropdownMenu.Item>
{/if}
