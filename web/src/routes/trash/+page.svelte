<script lang="ts">
	import { api, ok } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import { relativeTime } from '#lib/dates.ts';
	import { useView } from '#lib/view.svelte.ts';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import Markdown from '#lib/components/Markdown.svelte';

	useView(
		() => ({ name: 'Trash', href: '/trash', matches: (t) => !!t.deleted_at }),
		() => app.loadTasks({ deleted: 'true', sort: 'updated' })
	);
	const tasks = $derived(Object.values(app.tasks).filter((t) => t.deleted_at).sort((a, b) => (b.deleted_at ?? '').localeCompare(a.deleted_at ?? '')));

	async function restore(id: string) {
		app.upsert(await ok(api.POST('/tasks/{id}/restore', { params: { path: { id } } })));
		app.toast('Restored');
	}
	async function purge(id: string) {
		if (!confirm('Delete forever?')) return;
		await ok(api.DELETE('/tasks/{id}', { params: { path: { id }, query: { permanent: true } } }));
		delete app.tasks[id];
	}
</script>

<svelte:head><title>Trash · Blair</title></svelte:head>
<ViewHeader title="Trash" subtitle="Deleted tasks are removed for good after 30 days" />
<div class="list">
	{#each tasks as t (t.id)}
		<div class="row">
			<span class="title"><Markdown src={t.title} inline /></span>
			<span class="muted">{relativeTime(t.deleted_at!)}</span>
			<button class="btn" onclick={() => restore(t.id)}>Restore</button>
			<button class="btn btn-ghost btn-danger" onclick={() => purge(t.id)}>Delete forever</button>
		</div>
	{:else}
		<p class="muted">Trash is empty.</p>
	{/each}
</div>

<style>
	.list {
		max-width: 860px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 8px;
		border-bottom: 1px solid var(--border);
		flex-wrap: wrap;
	}
	.title {
		flex: 1;
		min-width: 200px;
	}
	.muted {
		color: var(--muted);
		font-size: 12.5px;
	}
</style>
