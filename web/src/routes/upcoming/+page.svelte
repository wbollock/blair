<script lang="ts">
	import { app, byDue, isOpen } from '#lib/state.svelte.ts';
	import { addDays, dueLabel, longDate } from '#lib/dates.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import Section from '#lib/components/Section.svelte';
	import TaskList from '#lib/components/TaskList.svelte';

	const tomorrow = $derived(addDays(app.today, 1));
	useView(
		() => ({ name: 'Upcoming', href: '/upcoming', dueDefault: tomorrow, matches: (t) => isOpen(t) && !!t.due_date && t.due_date > app.today }),
		() => app.loadTasks({ due_from: addDays(app.today, 1), status: 'todo,doing,waiting', sort: 'due' })
	);
	const tasks = $derived(Object.values(app.tasks).filter((t) => isOpen(t) && t.due_date && t.due_date > app.today).sort(byDue));
	const horizon = $derived(addDays(app.today, 14));
	const days = $derived.by(() => {
		const out: { date: string; tasks: typeof tasks }[] = [];
		for (let i = 1; i <= 14; i++) {
			const d = addDays(app.today, i);
			out.push({ date: d, tasks: tasks.filter((t) => t.due_date === d) });
		}
		return out;
	});
	const later = $derived(tasks.filter((t) => t.due_date! > horizon));
</script>

<svelte:head><title>Upcoming · Blair</title></svelte:head>

<ViewHeader title="Upcoming" subtitle="The next two weeks" />
<Page>
	{#each days as d (d.date)}
		{#if d.tasks.length || d.date === tomorrow}
			<Section title={`${dueLabel(d.date, null, app.tz)} · ${longDate(d.date)}`}>
				<TaskList tasks={d.tasks} empty="Nothing planned." />
			</Section>
		{/if}
	{/each}
	{#if later.length}
		<Section title="Later" count={later.length}><TaskList tasks={later} /></Section>
	{/if}
</Page>
