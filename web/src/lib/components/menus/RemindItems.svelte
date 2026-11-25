<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import { app } from '#lib/state.svelte.ts';
	import { addDays, isoToZoned, remindLabel, zonedToISO } from '#lib/dates.ts';
	import type { Task } from '#lib/api.ts';

	let { task }: { task: Task } = $props();
	const options = $derived.by(() => {
		const now = isoToZoned(new Date().toISOString(), app.tz);
		const out: { label: string; at: string }[] = [];
		if (task.due_date && task.due_time) out.push({ label: 'At due time', at: zonedToISO(task.due_date, task.due_time, app.tz) });
		out.push({ label: 'In 1 hour', at: new Date(Math.ceil((Date.now() + 3600_000) / 60_000) * 60_000).toISOString() });
		if (now.time < '17:00') out.push({ label: 'This evening', at: zonedToISO(now.date, '18:00', app.tz) });
		out.push({ label: 'Tomorrow morning', at: zonedToISO(addDays(now.date, 1), '09:00', app.tz) });
		return out.filter((o) => o.at > new Date().toISOString());
	});
	const set = (at: string | null, undo: string) => app.update(task.id, { remind_at: at }, { undo });
</script>

{#each options as o (o.label)}
	<DropdownMenu.Item class="menu-item" onSelect={() => set(o.at, `Reminder ${remindLabel(o.at, app.tz)}`)}>
		{o.label}<span class="kbd">{remindLabel(o.at, app.tz)}</span>
	</DropdownMenu.Item>
{/each}
<DropdownMenu.Item class="menu-item" onSelect={() => (app.drawer = task.id)}>Pick a time…</DropdownMenu.Item>
{#if task.remind_at}
	<DropdownMenu.Item class="menu-item" onSelect={() => set(null, 'Reminder removed')}>No reminder</DropdownMenu.Item>
{/if}
