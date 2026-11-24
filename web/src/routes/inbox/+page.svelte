<script lang="ts">
	import Sparkles from '@lucide/svelte/icons/sparkles';
	import { app, byPosition, isOpen } from '#lib/state.svelte.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import TaskList from '#lib/components/TaskList.svelte';

	const v = useView(
		() => ({ name: 'Inbox', href: '/inbox', matches: (t) => t.project_id === null && isOpen(t) }),
		() => app.loadTasks({ inbox: 'true', status: 'todo,doing,waiting' })
	);
	const tasks = $derived(Object.values(app.tasks).filter((t) => t.project_id === null && isOpen(t)).sort(byPosition));
</script>

<svelte:head><title>Inbox · Blair</title></svelte:head>

<ViewHeader title="Inbox" subtitle="Everything you haven't filed yet">
	{#snippet actions()}
		{#if tasks.length}<a class="btn" href="/triage"><Sparkles size={15} /> Triage {tasks.length}</a>{/if}
	{/snippet}
</ViewHeader>
<Page>
	<div class="spacer"></div>
	{#if !v.loading && !tasks.length}
		<div class="zero">
			<p class="big">Inbox zero.</p>
			<p>Capture with quick-add syntax: <code>Call dentist tomorrow 9am #life !high</code></p>
		</div>
	{:else}
		<TaskList {tasks} showProject={false} />
	{/if}
</Page>

<style>
	.spacer {
		height: 8px;
	}
	.zero {
		padding: 40px 8px;
		color: var(--muted);
	}
	.big {
		font-family: var(--font-display);
		font-size: 22px;
		color: var(--text);
		margin: 0 0 6px;
	}
</style>
