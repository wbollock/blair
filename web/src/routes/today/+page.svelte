<script lang="ts">
	import { app, byDue, isOpen } from '#lib/state.svelte.ts';
	import { longDate } from '#lib/dates.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import Section from '#lib/components/Section.svelte';
	import TaskList from '#lib/components/TaskList.svelte';

	const v = useView(
		() => ({ name: 'Today', href: '/today', dueDefault: app.today, matches: (t) => isOpen(t) && !!t.due_date && t.due_date <= app.today }),
		() => Promise.all([
			app.loadTasks({ due_to: app.today, status: 'todo,doing,waiting', sort: 'due' }),
			app.loadTasks({ status: 'done,cancelled', sort: 'completed', limit: '50' } as Record<string, string>)
		])
	);
	const all = $derived(Object.values(app.tasks));
	const overdue = $derived(all.filter((t) => isOpen(t) && t.due_date && t.due_date < app.today).sort(byDue));
	const today = $derived(all.filter((t) => isOpen(t) && t.due_date === app.today).sort(byDue));
	const planned = $derived(app.planEnabled ? app.plan.map((id) => app.tasks[id]).filter((t) => t && !t.deleted_at) : []);
	const doneToday = $derived(all.filter((t) => !t.deleted_at && t.completed_at && t.due_date && t.due_date <= app.today && new Date(t.completed_at).toDateString() === new Date().toDateString()));
</script>

<svelte:head><title>Today · Blair</title></svelte:head>

<ViewHeader title="Today" subtitle={longDate(app.today)} />
<Page>
	{#if planned.length}
		<Section title="Planned" count={planned.length}><TaskList tasks={planned} /></Section>
	{/if}
	{#if overdue.length}
		<Section title="Overdue" count={overdue.length} tone="var(--p4)"><TaskList tasks={overdue} /></Section>
	{/if}
	<Section title="Today" count={today.length}>
		<TaskList tasks={today} empty={v.loading ? '' : overdue.length ? 'Nothing else due today.' : 'Nothing due today. Add something above, or enjoy the quiet.'} />
	</Section>
	{#if doneToday.length}
		<Section title="Done today" count={doneToday.length}><TaskList tasks={doneToday} /></Section>
	{/if}
</Page>
