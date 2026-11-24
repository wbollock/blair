<script lang="ts">
	import type { Task } from '#lib/api.ts';
	import TaskRow from './TaskRow.svelte';

	let { tasks, showProject = true, empty = '' }: { tasks: Task[]; showProject?: boolean; empty?: string } = $props();
</script>

{#if tasks.length}
	<div class="list" role="list">
		{#each tasks as t (t.id)}
			<TaskRow task={t} {showProject} />
		{/each}
	</div>
{:else if empty}
	<p class="empty">{empty}</p>
{/if}

<style>
	.list {
		display: flex;
		flex-direction: column;
	}
	.empty {
		margin: 6px 8px 0;
		color: var(--muted);
		font-size: 13px;
	}
</style>
