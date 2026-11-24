<script lang="ts">
	import { app } from '#lib/state.svelte.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import TaskList from '#lib/components/TaskList.svelte';

	const since = new Date(Date.now() - 7 * 864e5).toISOString();
	useView(
		() => ({ name: 'Recently added', href: '/recent', matches: (t) => !t.deleted_at && t.created_at >= since }),
		() => app.loadTasks({ created_since: since, sort: 'created' })
	);
	const tasks = $derived(Object.values(app.tasks).filter((t) => !t.deleted_at && t.created_at >= since).sort((a, b) => b.created_at.localeCompare(a.created_at)));
</script>

<svelte:head><title>Recently added · Blair</title></svelte:head>
<ViewHeader title="Recently added" subtitle="Everything you captured in the last 7 days" />
<Page><TaskList {tasks} empty="Nothing added this week." /></Page>
