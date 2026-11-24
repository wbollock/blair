<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { DropdownMenu } from 'bits-ui';
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import List from '@lucide/svelte/icons/list';
	import Columns3 from '@lucide/svelte/icons/columns-3';
	import { api, ok } from '#lib/api.ts';
	import { app, byPosition } from '#lib/state.svelte.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import Section from '#lib/components/Section.svelte';
	import TaskList from '#lib/components/TaskList.svelte';
	import Board from '#lib/components/Board.svelte';

	const id = $derived(page.params.id as string);
	const project = $derived(app.project(id));
	let view = $state<'list' | 'board'>('list');
	let showDone = $state(false);
	let lastId = '';
	$effect(() => {
		if (project && lastId !== project.id) {
			lastId = project.id;
			view = project.default_view === 'board' ? 'board' : 'list';
		}
	});

	useView(
		() => ({ name: project?.name ?? 'Project', href: `/project/${id}`, projectId: id, matches: (t) => t.project_id === id && !t.deleted_at }),
		() => app.loadTasks({ project_id: id })
	);
	const tasks = $derived(Object.values(app.tasks).filter((t) => t.project_id === id && !t.deleted_at).sort(byPosition));

	async function setView(v: 'list' | 'board') {
		view = v;
		await api.PATCH('/projects/{id}', { params: { path: { id } }, body: { default_view: v } });
		await app.loadProjects();
	}
	async function rename() {
		const name = prompt('Rename project', project?.name);
		if (!name?.trim()) return;
		await ok(api.PATCH('/projects/{id}', { params: { path: { id } }, body: { name: name.trim() } }));
		await app.loadProjects();
	}
	async function archive() {
		await ok(api.PATCH('/projects/{id}', { params: { path: { id } }, body: { archived: true } }));
		await app.loadProjects();
		app.toast('Project archived');
		goto('/today');
	}
	async function remove() {
		if (!confirm(`Delete “${project?.name}” and all its tasks? This can't be undone.`)) return;
		await ok(api.DELETE('/projects/{id}', { params: { path: { id } } }));
		for (const t of tasks) delete app.tasks[t.id];
		await app.loadProjects();
		app.toast('Project deleted');
		goto('/today');
	}
	function onkeydown(e: KeyboardEvent) {
		const el = e.target as HTMLElement;
		if (e.key === 'b' && !e.metaKey && !e.ctrlKey && !['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName)) setView(view === 'list' ? 'board' : 'list');
	}
</script>

<svelte:window {onkeydown} />
<svelte:head><title>{project?.name ?? 'Project'} · Blair</title></svelte:head>

{#if !project}
	<p class="muted">Project not found.</p>
{:else}
	<ViewHeader title={project.name} subtitle={`${project.open_tasks} open`}>
		{#snippet actions()}
			<div class="seg" role="group" aria-label="View">
				<button aria-pressed={view === 'list'} class:on={view === 'list'} onclick={() => setView('list')}><List size={15} /> List</button>
				<button aria-pressed={view === 'board'} class:on={view === 'board'} onclick={() => setView('board')}><Columns3 size={15} /> Board</button>
			</div>
			<DropdownMenu.Root>
				<DropdownMenu.Trigger class="btn btn-ghost" aria-label="Project actions"><Ellipsis size={16} /></DropdownMenu.Trigger>
				<DropdownMenu.Portal>
					<DropdownMenu.Content class="menu" align="end" sideOffset={4}>
						<DropdownMenu.Item class="menu-item" onSelect={rename}>Rename</DropdownMenu.Item>
						<DropdownMenu.Item class="menu-item" onSelect={archive}>Archive</DropdownMenu.Item>
						<DropdownMenu.Separator class="menu-sep" />
						<DropdownMenu.Item class="menu-item danger" onSelect={remove}>Delete project</DropdownMenu.Item>
					</DropdownMenu.Content>
				</DropdownMenu.Portal>
			</DropdownMenu.Root>
		{/snippet}
	</ViewHeader>
	<Page wide={view === 'board'}>
		{#if view === 'board'}
			<div class="board-wrap"><Board {project} /></div>
		{:else}
			{#each project.columns as c (c.id)}
				{@const colTasks = tasks.filter((t) => t.column_id === c.id)}
				{#if c.status !== 'done' && c.status !== 'cancelled'}
					<Section title={c.name} count={colTasks.length}><TaskList tasks={colTasks} showProject={false} empty="Empty" /></Section>
				{:else if colTasks.length}
					<Section title={c.name} count={colTasks.length}>
						{#if showDone}<TaskList tasks={colTasks} showProject={false} />{:else}<button class="linkish" onclick={() => (showDone = true)}>Show {colTasks.length} completed</button>{/if}
					</Section>
				{/if}
			{/each}
		{/if}
	</Page>
{/if}

<style>
	.seg {
		display: flex;
		padding: 3px;
		gap: 2px;
		border-radius: var(--r);
		border: 1px solid var(--border);
		background: var(--surface);
	}
	.seg button {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		min-height: 30px;
		padding: 0 12px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
		font-size: 13px;
	}
	.seg .on {
		background: var(--selected);
		color: var(--text);
		font-weight: 600;
	}
	.board-wrap {
		margin-top: 18px;
	}
	.linkish {
		margin-left: 8px;
		border: 0;
		background: none;
		color: var(--muted);
		font-size: 13px;
	}
	.muted {
		color: var(--muted);
	}
</style>
