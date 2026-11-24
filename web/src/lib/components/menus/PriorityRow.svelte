<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import { app } from '#lib/state.svelte.ts';
	import type { Task } from '#lib/api.ts';

	let { task }: { task: Task } = $props();
	const names = ['None', 'Low', 'Medium', 'High', 'Urgent'];
</script>

<div class="menu-heading">Priority</div>
<div class="row" role="group" aria-label="Priority">
	{#each names as name, p (name)}
		<DropdownMenu.Item
			class="sw p{p}"
			aria-label={name}
			data-active={task.priority === p ? '' : undefined}
			onSelect={() => app.update(task.id, { priority: p })}
		/>
	{/each}
</div>

<style>
	.row {
		display: flex;
		gap: 6px;
		padding: 4px 10px 8px;
	}
	.row :global(.sw) {
		width: 32px;
		height: 26px;
		border-radius: var(--r-sm);
		border: 1.5px solid var(--border-strong);
		cursor: pointer;
		outline: none;
	}
	.row :global(.sw[data-highlighted]) {
		box-shadow: var(--focus-ring);
	}
	.row :global(.sw.p1) { border-color: var(--p1); background: var(--p1-soft); }
	.row :global(.sw.p2) { border-color: var(--p2); background: var(--p2-soft); }
	.row :global(.sw.p3) { border-color: var(--p3); background: var(--p3-soft); }
	.row :global(.sw.p4) { border-color: var(--p4); background: var(--p4-soft); }
	.row :global(.sw[data-active]) {
		outline: 2px solid var(--text);
		outline-offset: 1px;
	}
</style>
