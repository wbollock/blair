<script lang="ts">
	import { page } from '$app/state';
	import { app, byDue, isOpen } from '#lib/state.svelte.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import TaskList from '#lib/components/TaskList.svelte';

	const id = $derived(page.params.id as string);
	const label = $derived(app.label(id));
	useView(
		() => ({ name: label ? `@${label.name}` : 'Label', href: `/label/${id}`, labelId: id, matches: (t) => isOpen(t) && t.labels.includes(id) }),
		() => app.loadTasks({ label_id: id, status: 'todo,doing,waiting' })
	);
	const tasks = $derived(Object.values(app.tasks).filter((t) => isOpen(t) && t.labels.includes(id)).sort(byDue));
</script>

<svelte:head><title>@{label?.name ?? 'label'} · Blair</title></svelte:head>
<ViewHeader title={label ? `@${label.name}` : 'Label'} />
<Page><TaskList {tasks} empty="No open tasks with this label." /></Page>
