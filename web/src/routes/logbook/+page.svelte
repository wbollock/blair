<script lang="ts">
	import { app } from '#lib/state.svelte.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import TaskList from '#lib/components/TaskList.svelte';

	const closed = (s: string) => s === 'done' || s === 'cancelled';
	useView(
		() => ({ name: 'Logbook', href: '/logbook', matches: (t) => closed(t.status) && !t.deleted_at }),
		() => app.loadTasks({ status: 'done,cancelled', sort: 'completed' })
	);
	const tasks = $derived(Object.values(app.tasks).filter((t) => closed(t.status) && !t.deleted_at).sort((a, b) => (b.completed_at ?? '').localeCompare(a.completed_at ?? '')));
</script>

<svelte:head><title>Logbook · Blair</title></svelte:head>
<ViewHeader title="Logbook" subtitle="Completed and cancelled tasks" />
<Page quickAdd={false}><TaskList {tasks} empty="Nothing completed yet." /></Page>
