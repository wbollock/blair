<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { app, byPosition, isOpen } from '#lib/state.svelte.ts';
	import { dueLabel } from '#lib/dates.ts';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import Checkbox from '#lib/components/Checkbox.svelte';
	import Markdown from '#lib/components/Markdown.svelte';
	import FieldMenu from '#lib/components/FieldMenu.svelte';
	import ProjectItems from '#lib/components/menus/ProjectItems.svelte';
	import ColumnItems from '#lib/components/menus/ColumnItems.svelte';
	import DueItems from '#lib/components/menus/DueItems.svelte';
	import PriorityRow from '#lib/components/menus/PriorityRow.svelte';
	import LabelItems from '#lib/components/menus/LabelItems.svelte';

	// Snapshot the inbox at start so filing a task doesn't reshuffle the queue.
	let queue = $state<string[]>([]);
	let i = $state(0);
	let loaded = $state(false);
	onMount(async () => {
		const tasks = await app.loadTasks({ inbox: 'true', status: 'todo,doing,waiting' });
		queue = tasks.sort(byPosition).map((t) => t.id);
		loaded = true;
	});
	const task = $derived(queue[i] ? app.tasks[queue[i]] : undefined);
	const project = $derived(app.project(task?.project_id));
	const column = $derived(app.column(task?.column_id));
	const suggestions = $derived.by(() => {
		if (!task) return [];
		const words = new Set(task.title.toLowerCase().split(/\W+/).filter((w) => w.length > 2));
		return app.projects
			.filter((p) => !p.archived_at)
			.map((p) => ({ p, score: p.name.toLowerCase().split(/\W+/).filter((w) => words.has(w)).length }))
			.sort((a, b) => b.score - a.score)
			.slice(0, 4)
			.map((x) => x.p);
	});
	const priorityNames = ['Priority', 'Low', 'Medium', 'High', 'Urgent'];

	function next() {
		i = Math.min(i + 1, queue.length);
	}
	function prev() {
		i = Math.max(i - 1, 0);
	}
	function onkeydown(e: KeyboardEvent) {
		if (!task || ['INPUT', 'TEXTAREA'].includes((e.target as HTMLElement).tagName) || (e.target as HTMLElement).closest('[role="menu"]')) return;
		const k = e.key;
		if (k === 'j' || k === 'Enter' || k === 's' || k === 'ArrowRight') next();
		else if (k === 'k' || k === 'ArrowLeft') prev();
		else if (k === 'x') app.toggleDone(task.id).then(next);
		else if (k === '#' || k === 'Delete') app.remove(task.id).then(() => (queue = queue.filter((x) => x !== task.id)));
		else if (/^[0-4]$/.test(k)) app.update(task.id, { priority: Number(k) });
		else if (k === 'Escape') goto('/inbox');
		else return;
		e.preventDefault();
	}
</script>

<svelte:window {onkeydown} />
<svelte:head><title>Triage · Blair</title></svelte:head>

<ViewHeader title="Triage" subtitle={queue.length ? `${Math.min(i + 1, queue.length)} of ${queue.length}` : ''} />
<div class="wrap">
	{#if !loaded}
		<p class="muted">Loading…</p>
	{:else if !task}
		<div class="zero">
			<p class="big">{queue.length ? 'All filed.' : 'Inbox zero.'}</p>
			<a class="btn" href="/today">Go to Today</a>
		</div>
	{:else}
		<div class="progress"><span style="width: {(i / queue.length) * 100}%"></span></div>
		<article class="card">
			<div class="top">
				<Checkbox done={!isOpen(task)} priority={task.priority} size={24} onclick={() => app.toggleDone(task.id)} />
				<h2><Markdown src={task.title} inline /></h2>
			</div>
			{#if task.description}<div class="desc"><Markdown src={task.description} /></div>{/if}
			{#if suggestions.length}
				<div class="sugg">
					<span class="eyebrow">File into</span>
					{#each suggestions as p (p.id)}
						<button class="chip" class:set={task.project_id === p.id} onclick={() => app.update(task.id, { project_id: p.id })}>
							<span class="dot" style="background: {p.color || 'var(--accent)'}"></span>{p.name}
						</button>
					{/each}
				</div>
			{/if}
			<div class="chips">
				<FieldMenu set={!!project} ariaLabel="Project">
					{#snippet trigger()}<span class="dot" style="background: {project?.color || 'var(--faint)'}"></span>{project?.name ?? 'Inbox'} ▾{/snippet}
					{#snippet items()}<ProjectItems {task} />{/snippet}
				</FieldMenu>
				{#if project}
					<FieldMenu set label={column?.name ?? 'Column'}>{#snippet items()}<ColumnItems {task} />{/snippet}</FieldMenu>
				{/if}
				<FieldMenu set={!!task.due_date} label={task.due_date ? dueLabel(task.due_date, task.due_time, app.tz) : 'Due'}>{#snippet items()}<DueItems {task} />{/snippet}</FieldMenu>
				<FieldMenu set={task.priority > 0} label={priorityNames[task.priority]}>{#snippet items()}<PriorityRow {task} />{/snippet}</FieldMenu>
				<FieldMenu set={task.labels.length > 0} label={task.labels.length ? `${task.labels.length} label${task.labels.length > 1 ? 's' : ''}` : 'Labels'}>{#snippet items()}<LabelItems {task} />{/snippet}</FieldMenu>
			</div>
			<div class="actions">
				<button class="btn btn-primary" onclick={next}>{task.project_id ? 'File & next' : 'Next'} <kbd>↵</kbd></button>
				<button class="btn" onclick={() => app.toggleDone(task.id).then(next)}>Done <kbd>x</kbd></button>
				<button class="btn" onclick={() => (app.drawer = task.id)}>Details</button>
				<button class="btn btn-ghost btn-danger" onclick={() => app.remove(task.id).then(() => (queue = queue.filter((x) => x !== task.id)))}>Delete</button>
				<span class="sp"></span>
				<button class="btn btn-ghost" disabled={i === 0} onclick={prev}>← Back</button>
			</div>
		</article>
		<p class="keys muted">j/↵ next · k back · x done · 0–4 priority · # delete · Esc leave</p>
	{/if}
</div>

<style>
	.wrap {
		max-width: 680px;
	}
	.progress {
		height: 4px;
		border-radius: 99px;
		background: var(--chip);
		margin-bottom: 16px;
		overflow: hidden;
	}
	.progress span {
		display: block;
		height: 100%;
		background: var(--accent);
		transition: width 240ms var(--ease);
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: 22px;
		border-radius: var(--r);
		background: var(--surface);
		border: 1px solid var(--border);
		box-shadow: var(--shadow-card);
	}
	.top {
		display: flex;
		gap: 14px;
		align-items: center;
	}
	h2 {
		margin: 0;
		font-family: var(--font-display);
		font-weight: 600;
		font-size: 24px;
		line-height: 1.25;
	}
	.desc {
		color: var(--muted);
	}
	.sugg,
	.chips,
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
	}
	.sugg .chip,
	.chips :global(.chip) {
		min-height: 36px;
		padding: 0 12px;
		font-size: 13.5px;
	}
	.sp {
		flex: 1;
	}
	kbd {
		font-family: var(--font-mono);
		font-size: 11px;
		opacity: 0.7;
	}
	.keys {
		margin-top: 12px;
	}
	.zero {
		padding: 40px 0;
	}
	.big {
		font-family: var(--font-display);
		font-size: 24px;
		margin: 0 0 12px;
	}
	.muted {
		color: var(--muted);
		font-size: 12.5px;
	}
</style>
