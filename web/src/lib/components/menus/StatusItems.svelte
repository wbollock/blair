<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import { app } from '#lib/state.svelte.ts';
	import type { Status, Task } from '#lib/api.ts';

	let { task }: { task: Task } = $props();
	const statuses: { s: Status; label: string }[] = [
		{ s: 'todo', label: 'To do' },
		{ s: 'doing', label: 'Doing' },
		{ s: 'waiting', label: 'Waiting' },
		{ s: 'done', label: 'Done' },
		{ s: 'cancelled', label: 'Cancelled' }
	];
	function target(s: Status) {
		return app.project(task.project_id)?.columns.find((c) => c.status === s)?.name;
	}
</script>

{#each statuses as { s, label } (s)}
	<DropdownMenu.Item class="menu-item" onSelect={() => app.update(task.id, { status: s }, { undo: `Marked ${label.toLowerCase()}` })}>
		<span>{label}</span>
		<span class="kbd">{task.status === s ? '✓' : (target(s) ?? '')}</span>
	</DropdownMenu.Item>
{/each}
