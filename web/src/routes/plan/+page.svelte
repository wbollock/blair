<script lang="ts">
	import Plus from '@lucide/svelte/icons/plus';
	import X from '@lucide/svelte/icons/x';
	import Copy from '@lucide/svelte/icons/copy';
	import { api, ok, type Schemas, type Task } from '#lib/api.ts';
	import { app, isOpen } from '#lib/state.svelte.ts';
	import { addDays, longDate } from '#lib/dates.ts';
	import { copyRich, standup } from '#lib/standup.ts';
	import { useView } from '#lib/view.svelte.ts';
	import Page from '#lib/components/Page.svelte';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import Section from '#lib/components/Section.svelte';
	import TaskRow from '#lib/components/TaskRow.svelte';

	// Pick today's work (ADR-0014). Candidates are open tasks, optionally from one project.
	let slipping = $state<Schemas['Slipping'][]>([]);
	let carry = $state<string[]>([]);
	let projectFilter = $state(readFilter());
	function readFilter(): string {
		try {
			return localStorage.getItem('blair.plan.project') ?? '';
		} catch {
			return '';
		}
	}
	$effect(() => {
		try {
			localStorage.setItem('blair.plan.project', projectFilter);
		} catch {
			/* storage unavailable */
		}
	});

	const v = useView(
		() => ({ name: 'Plan', href: '/plan', matches: (t) => app.inPlan(t.id) }),
		async () => {
			const yesterday = addDays(app.today, -1);
			const [, , slip, prev] = await Promise.all([
				app.loadPlan(),
				app.loadTasks({ status: 'todo,doing,waiting' }),
				ok(api.GET('/plans/slipping', { params: { query: { min_days: 2 } } })),
				ok(api.GET('/plans/{date}', { params: { path: { date: yesterday } } }))
			]);
			slipping = slip.data;
			for (const t of prev.tasks) app.upsert(t);
			carry = prev.task_ids;
		}
	);

	const planned = $derived(app.plan.map((id) => app.tasks[id]).filter((t): t is Task => !!t && !t.deleted_at));
	const carryOver = $derived(carry.filter((id) => !app.inPlan(id) && app.tasks[id] && isOpen(app.tasks[id])));
	const slipCount = $derived(new Map(slipping.map((s) => [s.task.id, s.planned_days])));
	const rank = (t: Task) => ({ doing: 0, waiting: 2 })[t.status as string] ?? 1;
	const candidates = $derived(
		Object.values(app.tasks)
			.filter((t) => isOpen(t) && !app.inPlan(t.id) && (!projectFilter || (projectFilter === 'inbox' ? t.project_id === null : t.project_id === projectFilter)))
			.sort((a, b) => rank(a) - rank(b) || b.priority - a.priority || (a.due_date ?? '9999').localeCompare(b.due_date ?? '9999'))
	);

	async function copy() {
		await copyRich(standup(planned));
		app.toast('Copied for standup');
	}
</script>

<svelte:head><title>Plan · Blair</title></svelte:head>

{#snippet slipBadge(id: string)}
	{#if slipCount.get(id)}<span class="slip" title="Planned on {slipCount.get(id)} earlier days without finishing">planned {slipCount.get(id)}×</span>{/if}
{/snippet}

<ViewHeader title="Plan" subtitle={longDate(app.today)}>
	{#snippet actions()}
		{#if planned.length}<button class="btn" onclick={copy}><Copy size={15} /> Copy for standup</button>{/if}
	{/snippet}
</ViewHeader>
<Page>
	{#if !app.planEnabled}
		<p class="off">The daily plan is turned off. <a href="/settings">Turn it on in Settings</a> to see it on Today and in menus.</p>
	{/if}

	<Section title="Today" count={planned.length}>
		<div role="list">
			{#each planned as t (t.id)}
				<div class="r">
					<div class="task"><TaskRow task={t} /></div>
					{@render slipBadge(t.id)}
					<button class="icon" aria-label="Remove from today's plan" title="Remove from plan" onclick={() => app.togglePlan(t.id, { quiet: true })}><X size={15} /></button>
				</div>
			{:else}
				<p class="empty">{v.loading ? '' : 'Nothing planned yet. Add tasks from below.'}</p>
			{/each}
		</div>
		{#if carryOver.length}
			<button class="carry" onclick={() => app.addToPlan(carryOver)}>Carry over {carryOver.length} unfinished from yesterday</button>
		{/if}
	</Section>

	{#if slipping.some((s) => !app.inPlan(s.task.id))}
		<Section title="Keeps slipping" tone="var(--p4)">
			<p class="how">Planned on several days but not finished. Do it, break it down, or drop it.</p>
			<div role="list">
				{#each slipping.filter((s) => !app.inPlan(s.task.id)) as s (s.task.id)}
					{@const t = app.tasks[s.task.id] ?? s.task}
					<div class="r">
						<div class="task"><TaskRow task={t} /></div>
						{@render slipBadge(t.id)}
						<button class="add" onclick={() => app.togglePlan(t.id, { quiet: true })}><Plus size={14} /> Plan</button>
					</div>
				{/each}
			</div>
		</Section>
	{/if}

	<Section title="Pick from" count={candidates.length}>
		<select class="input filter" bind:value={projectFilter} aria-label="Only show tasks from">
			<option value="">All projects</option>
			<option value="inbox">Inbox</option>
			{#each app.projects.filter((p) => !p.archived_at) as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
		</select>
		<div role="list">
			{#each candidates as t (t.id)}
				<div class="r">
					<div class="task"><TaskRow task={t} /></div>
					{@render slipBadge(t.id)}
					<button class="add" onclick={() => app.togglePlan(t.id, { quiet: true })}><Plus size={14} /> Plan</button>
				</div>
			{/each}
		</div>
	</Section>
</Page>

<style>
	.off,
	.how,
	.empty {
		margin: 0 0 12px;
		font-size: 13px;
		color: var(--muted);
	}
	.r {
		display: flex;
		align-items: center;
		gap: 8px;
		border-bottom: 1px solid var(--border);
	}
	.task {
		flex: 1;
		min-width: 0;
	}
	.task :global(.row) {
		border-bottom: 0;
	}
	.slip {
		flex: none;
		font-size: 11px;
		font-weight: 600;
		color: var(--p4);
	}
	.add,
	.carry {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 4px;
		min-height: 28px;
		padding: 0 10px;
		border: 1px solid var(--border);
		border-radius: var(--r-sm);
		background: none;
		font-size: 12px;
		color: var(--muted);
	}
	.add:hover,
	.carry:hover {
		background: var(--hover);
		color: var(--text);
	}
	.carry {
		margin-top: 10px;
	}
	.icon {
		flex: none;
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
	}
	.filter {
		max-width: 260px;
		margin-bottom: 8px;
	}
</style>
