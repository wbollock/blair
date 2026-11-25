<script lang="ts">
	import { app, byDue, isOpen } from '#lib/state.svelte.ts';
	import { addDays } from '#lib/dates.ts';
	import type { Task } from '#lib/api.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import Section from '#lib/components/Section.svelte';
	import TaskRow from '#lib/components/TaskRow.svelte';

	// A weekly sweep for tasks that are drifting: unfiled, overdue, stuck, or forgotten.
	const v = useView(
		() => ({ name: 'Review', href: '/review', matches: () => false }),
		() => app.loadTasks({ status: 'todo,doing,waiting' })
	);
	const ago = (days: number) => new Date(Date.now() - days * 864e5).toISOString();
	const open = $derived(Object.values(app.tasks).filter(isOpen));
	const inbox = $derived(open.filter((t) => t.project_id === null).sort(byDue));
	const overdue = $derived(open.filter((t) => t.project_id !== null && t.due_date && t.due_date < app.today).sort(byDue));
	const stuck = $derived(open.filter((t) => (t.status === 'doing' || t.status === 'waiting') && t.updated_at < ago(7) && !(t.due_date && t.due_date < app.today)).sort((a, b) => a.updated_at.localeCompare(b.updated_at)));
	const forgotten = $derived(open.filter((t) => t.status === 'todo' && t.project_id !== null && t.updated_at < ago(30) && !(t.due_date && t.due_date <= addDays(app.today, 7))).sort((a, b) => a.updated_at.localeCompare(b.updated_at)));
	const total = $derived(inbox.length + overdue.length + stuck.length + forgotten.length);

	const idle = (t: Task) => Math.floor((Date.now() - new Date(t.updated_at).getTime()) / 864e5);
	// An empty PATCH bumps updated_at, which takes the task off the stale lists.
	const keep = (t: Task) => app.update(t.id, {}).then((r) => r && app.toast('Marked as reviewed'));
	const snooze = (t: Task) => app.update(t.id, { due_date: addDays(t.due_date && t.due_date > app.today ? t.due_date : app.today, 7) }, { undo: 'Snoozed a week' });
</script>

<svelte:head><title>Weekly review · Blair</title></svelte:head>

{#snippet rows(tasks: Task[], showIdle: boolean)}
	<div role="list">
		{#each tasks as t (t.id)}
			<div class="r">
				<div class="task"><TaskRow task={t} /></div>
				<div class="acts">
					{#if showIdle}<span class="idle">{idle(t)} d</span>{/if}
					<button onclick={() => keep(t)} title="Still relevant: mark it as reviewed">Keep</button>
					<button onclick={() => snooze(t)} title="Move the due date a week out">Snooze 1 wk</button>
				</div>
			</div>
		{/each}
	</div>
{/snippet}

<ViewHeader title="Weekly review" subtitle={v.loading ? 'Gathering…' : total ? `${total} task${total === 1 ? '' : 's'} need a decision` : 'Nothing is drifting.'} />
<Page>
	<p class="how">For each task: finish it, file it, snooze it, delete it, or press <b>Keep</b> to confirm it still matters.</p>
	{#if inbox.length}<Section title="Not filed" count={inbox.length}>{@render rows(inbox, false)}</Section>{/if}
	{#if overdue.length}<Section title="Overdue" count={overdue.length} tone="var(--p4)">{@render rows(overdue, false)}</Section>{/if}
	{#if stuck.length}<Section title="In progress, no change for a week" count={stuck.length}>{@render rows(stuck, true)}</Section>{/if}
	{#if forgotten.length}<Section title="Untouched for 30+ days" count={forgotten.length}>{@render rows(forgotten, true)}</Section>{/if}
	{#if !v.loading && !total}
		<div class="zero"><p class="big">All clear.</p><p>Every open task is filed, on time and moving.</p></div>
	{/if}
</Page>

<style>
	.how {
		margin: 0 0 18px;
		font-size: 13px;
		color: var(--muted);
	}
	.r {
		display: flex;
		align-items: center;
		gap: 8px;
		border-bottom: 1px solid var(--border);
	}
	.task {
		flex: 1;
		min-width: 0;
	}
	.task :global(.row) {
		border-bottom: 0;
	}
	.acts {
		display: flex;
		align-items: center;
		gap: 4px;
		flex: none;
	}
	.acts button {
		min-height: 28px;
		padding: 0 8px;
		border: 1px solid var(--border);
		border-radius: var(--r-sm);
		background: none;
		font-size: 12px;
		color: var(--muted);
	}
	.acts button:hover {
		background: var(--hover);
		color: var(--text);
	}
	.idle {
		font-size: 12px;
		color: var(--faint);
		margin-right: 4px;
	}
	.zero {
		padding: 40px 0;
		text-align: center;
		color: var(--muted);
	}
	.big {
		font-family: var(--font-display);
		font-size: 22px;
		color: var(--text);
	}
	@media (max-width: 640px) {
		.r {
			flex-direction: column;
			align-items: stretch;
			gap: 0;
			padding-bottom: 8px;
		}
		.acts {
			justify-content: flex-end;
		}
	}
</style>
