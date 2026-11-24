<script lang="ts">
	import X from '@lucide/svelte/icons/x';
	import { app } from '#lib/state.svelte.ts';
	import { dueLabel } from '#lib/dates.ts';
	import Checkbox from './Checkbox.svelte';
	import FieldMenu from './FieldMenu.svelte';
	import Markdown from './Markdown.svelte';
	import ProjectItems from './menus/ProjectItems.svelte';
	import ColumnItems from './menus/ColumnItems.svelte';
	import DueItems from './menus/DueItems.svelte';
	import PriorityRow from './menus/PriorityRow.svelte';
	import LabelItems from './menus/LabelItems.svelte';

	let { max = 3 }: { max?: number } = $props();
	const items = $derived(app.justAdded.map((id) => app.tasks[id]).filter(Boolean));
	const shown = $derived(items.slice(0, max));
	const priorityNames = ['Priority', 'Low', 'Medium', 'High', 'Urgent'];

	function where(t: (typeof items)[number]) {
		return app.project(t.project_id)?.name ?? 'Inbox';
	}
	function hrefFor(t: (typeof items)[number]) {
		return t.project_id ? `/project/${t.project_id}` : '/inbox';
	}
</script>

{#if shown.length}
	<section class="tray" aria-label="Just added">
		<div class="head">
			<span class="eyebrow">Just added</span>
			<button class="clear" onclick={() => (app.justAdded = [])}>Clear</button>
		</div>
		{#each shown as t (t.id)}
			{@const hidden = app.view && !app.view.matches(t)}
			{@const project = app.project(t.project_id)}
			{@const column = app.column(t.column_id)}
			<article class="card" class:pending={t.version === 0}>
				<div class="top">
					<Checkbox done={t.status === 'done' || t.status === 'cancelled'} priority={t.priority} onclick={() => app.toggleDone(t.id)} />
					<button class="title" onclick={() => (app.drawer = t.id)}><Markdown src={t.title} inline /></button>
					{#if t.version === 0}<span class="note">saving…</span>{/if}
					<button class="x" aria-label="Dismiss" onclick={() => (app.justAdded = app.justAdded.filter((x) => x !== t.id))}><X size={14} /></button>
				</div>
				{#if hidden}
					<div class="notice">
						Added to <b>{where(t)}</b> · not shown in {app.view?.name}
						<a href={hrefFor(t)}>Go to it</a>
						{#if app.view?.name === 'Today' && t.due_date !== app.today}
							<button class="linkish" onclick={() => app.update(t.id, { due_date: app.today })}>Set due today</button>
						{/if}
					</div>
				{/if}
				<div class="chips">
					<FieldMenu set={!!project} ariaLabel="Project">
						{#snippet trigger()}<span class="dot" style="background: {project?.color || 'var(--faint)'}"></span>{project?.name ?? 'Inbox'} ▾{/snippet}
						{#snippet items()}<ProjectItems task={t} />{/snippet}
					</FieldMenu>
					{#if project}
						<FieldMenu set={!!column} label={column?.name ?? 'Column'} ariaLabel="Column">
							{#snippet items()}<ColumnItems task={t} />{/snippet}
						</FieldMenu>
					{/if}
					<FieldMenu set={!!t.due_date} label={t.due_date ? dueLabel(t.due_date, t.due_time, app.tz) : 'Due'} ariaLabel="Due date">
						{#snippet items()}<DueItems task={t} />{/snippet}
					</FieldMenu>
					<FieldMenu set={t.priority > 0} label={priorityNames[t.priority]} ariaLabel="Priority">
						{#snippet items()}<PriorityRow task={t} />{/snippet}
					</FieldMenu>
					<FieldMenu set={t.labels.length > 0} label={t.labels.length ? t.labels.map((id) => '@' + (app.label(id)?.name ?? '')).join(' ') : 'Labels'} ariaLabel="Labels">
						{#snippet items()}<LabelItems task={t} />{/snippet}
					</FieldMenu>
					<span class="sp"></span>
					<button class="details" onclick={() => (app.drawer = t.id)}>Details →</button>
				</div>
			</article>
		{/each}
		{#if items.length > max}
			<a class="more" href="/recent">+{items.length - max} more · Recently added</a>
		{/if}
	</section>
{/if}

<style>
	.tray {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.head {
		display: flex;
		justify-content: space-between;
		align-items: center;
	}
	.clear,
	.linkish {
		border: 0;
		background: none;
		padding: 0;
		font-size: 12px;
		color: var(--muted);
	}
	.linkish {
		color: var(--accent-ink);
		font-weight: 600;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 12px 14px;
		border-radius: var(--r);
		background: var(--surface);
		border: 1px solid var(--border);
		box-shadow: var(--shadow-card);
		animation: arrive 260ms var(--ease);
	}
	.card.pending {
		opacity: 0.85;
	}
	@keyframes arrive {
		from {
			opacity: 0;
			transform: translateY(-6px) scale(0.985);
			box-shadow: 0 0 0 3px var(--accent-soft);
		}
	}
	.top {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.title {
		flex: 1;
		min-width: 0;
		text-align: left;
		padding: 0;
		border: 0;
		background: none;
		font-size: 15px;
		overflow-wrap: anywhere;
	}
	.note {
		font-size: 12px;
		color: var(--faint);
	}
	.x {
		display: grid;
		place-items: center;
		width: 26px;
		height: 26px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--faint);
	}
	.x:hover {
		background: var(--chip);
	}
	.notice {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		align-items: center;
		margin-left: 30px;
		padding: 6px 10px;
		border-radius: var(--r-sm);
		background: var(--accent-soft);
		font-size: 12.5px;
	}
	.notice a {
		font-weight: 600;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		padding-left: 30px;
		align-items: center;
	}
	.sp {
		flex: 1;
	}
	.details {
		border: 0;
		background: none;
		font-size: 12.5px;
		font-weight: 600;
		color: var(--accent-ink);
		min-height: 28px;
	}
	.more {
		font-size: 12.5px;
		color: var(--muted);
	}
	@media (max-width: 640px) {
		.chips,
		.notice {
			padding-left: 0;
			margin-left: 0;
		}
		.chips :global(.chip) {
			min-height: 34px;
		}
	}
</style>
