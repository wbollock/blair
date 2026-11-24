<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import Plus from '@lucide/svelte/icons/plus';
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import { api, ok, type Project, type Status, type Task } from '#lib/api.ts';
	import { app, byPosition } from '#lib/state.svelte.ts';
	import TaskRow from './TaskRow.svelte';

	let { project }: { project: Project } = $props();
	let showOldDone = $state(false);
	let dropCol = $state<string | null>(null);
	let adding = $state<string | null>(null);
	let addText = $state('');
	let renaming = $state<string | null>(null);
	let renameText = $state('');
	let newCol = $state('');

	const cutoff = $derived(new Date(Date.now() - 14 * 864e5).toISOString());
	const byColumn = $derived.by(() => {
		const m: Record<string, Task[]> = {};
		for (const c of project.columns) m[c.id] = [];
		for (const t of Object.values(app.tasks)) {
			if (t.project_id !== project.id || t.deleted_at || !t.column_id || !m[t.column_id]) continue;
			if (!showOldDone && t.completed_at && t.completed_at < cutoff) continue;
			m[t.column_id].push(t);
		}
		for (const k in m) m[k].sort(byPosition);
		return m;
	});

	function ondragover(e: DragEvent, colId: string) {
		if (!app.dragId) return;
		e.preventDefault();
		dropCol = colId;
	}
	function ondrop(e: DragEvent, colId: string) {
		e.preventDefault();
		const id = app.dragId;
		app.dragId = dropCol = null;
		if (!id) return;
		// Place before the card under the pointer, else append.
		const cards = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[data-task-id]')].filter((el) => el.dataset.taskId !== id);
		const before = cards.find((el) => {
			const r = el.getBoundingClientRect();
			return e.clientY < r.top + r.height / 2;
		});
		app.move(id, { column_id: colId, ...(before ? { before_id: before.dataset.taskId } : {}) });
	}
	async function addTask(colId: string) {
		const text = addText.trim();
		if (!text) return;
		addText = '';
		const t = await app.createTask({ quick_add: text, context_project_id: project.id, column_id: colId, create_missing: true });
		if (t) app.justAdded = [t.id, ...app.justAdded].slice(0, 8);
	}
	async function columnPatch(id: string, body: { name?: string; status?: Status }) {
		try {
			await ok(api.PATCH('/columns/{id}', { params: { path: { id } }, body }));
			await app.loadProjects();
			if (body.status) await app.loadTasks({ project_id: project.id });
		} catch (e) {
			app.toast(e instanceof Error ? e.message : 'Could not update column', 'error');
		}
	}
	async function deleteColumn(id: string) {
		const others = project.columns.filter((c) => c.id !== id);
		const target = others.find((c) => c.status === 'todo') ?? others[0];
		const hasTasks = (byColumn[id] ?? []).length > 0;
		try {
			await ok(api.DELETE('/columns/{id}', { params: { path: { id }, query: hasTasks && target ? { move_tasks_to: target.id } : {} } }));
			await app.loadProjects();
			await app.loadTasks({ project_id: project.id });
			app.toast(hasTasks && target ? `Column deleted · tasks moved to ${target.name}` : 'Column deleted');
		} catch (e) {
			app.toast(e instanceof Error ? e.message : 'Could not delete column', 'error');
		}
	}
	async function addColumn() {
		const name = newCol.trim();
		if (!name) return;
		newCol = '';
		try {
			await ok(api.POST('/projects/{id}/columns', { params: { path: { id: project.id } }, body: { name, status: 'doing' } }));
			await app.loadProjects();
		} catch (e) {
			app.toast(e instanceof Error ? e.message : 'Could not add column', 'error');
		}
	}
	const statusList: Status[] = ['todo', 'doing', 'waiting', 'done', 'cancelled'];
</script>

<div class="board" role="list" aria-label="Board">
	{#each project.columns as c (c.id)}
		{@const tasks = byColumn[c.id] ?? []}
		<section class="col" class:drop={dropCol === c.id} aria-label={c.name} ondragover={(e) => ondragover(e, c.id)} ondragleave={() => (dropCol = null)} ondrop={(e) => ondrop(e, c.id)}>
			<header>
				<span class="sd s-{c.status}" title={c.status}></span>
				{#if renaming === c.id}
					<!-- svelte-ignore a11y_autofocus -->
					<input class="input rename" bind:value={renameText} autofocus aria-label="Column name"
						onkeydown={(e) => { if (e.key === 'Enter') { columnPatch(c.id, { name: renameText }); renaming = null; } else if (e.key === 'Escape') renaming = null; }}
						onblur={() => { if (renameText.trim() && renameText !== c.name) columnPatch(c.id, { name: renameText }); renaming = null; }} />
				{:else}
					<button class="name" ondblclick={() => { renaming = c.id; renameText = c.name; }}>{c.name}</button>
				{/if}
				<span class="count" class:over={c.wip_limit && tasks.length > c.wip_limit}>{tasks.length}{c.wip_limit ? `/${c.wip_limit}` : ''}</span>
				<DropdownMenu.Root>
					<DropdownMenu.Trigger class="icon" aria-label="Column actions"><Ellipsis size={16} /></DropdownMenu.Trigger>
					<DropdownMenu.Portal>
						<DropdownMenu.Content class="menu" align="end" sideOffset={4}>
							<DropdownMenu.Item class="menu-item" onSelect={() => { renaming = c.id; renameText = c.name; }}>Rename</DropdownMenu.Item>
							<DropdownMenu.Sub>
								<DropdownMenu.SubTrigger class="menu-item">Status<span class="kbd">{c.status} ›</span></DropdownMenu.SubTrigger>
								<DropdownMenu.SubContent class="menu" sideOffset={6}>
									{#each statusList as s (s)}
										<DropdownMenu.Item class="menu-item" onSelect={() => columnPatch(c.id, { status: s })}>{s}{#if c.status === s}<span class="kbd">✓</span>{/if}</DropdownMenu.Item>
									{/each}
								</DropdownMenu.SubContent>
							</DropdownMenu.Sub>
							<DropdownMenu.Separator class="menu-sep" />
							<DropdownMenu.Item class="menu-item danger" onSelect={() => deleteColumn(c.id)}>Delete column</DropdownMenu.Item>
						</DropdownMenu.Content>
					</DropdownMenu.Portal>
				</DropdownMenu.Root>
			</header>
			<div class="cards" role="list">
				{#each tasks as t (t.id)}
					<div class="card" class:dragging={app.dragId === t.id}>
						<TaskRow task={t} showProject={false} />
					</div>
				{/each}
			</div>
			{#if adding === c.id}
				<form class="add" onsubmit={(e) => { e.preventDefault(); addTask(c.id); }}>
					<!-- svelte-ignore a11y_autofocus -->
					<input class="input" bind:value={addText} autofocus placeholder="Task title… (#, @, !, dates work)" aria-label="New task in {c.name}"
						onkeydown={(e) => e.key === 'Escape' && (adding = null)} onblur={() => !addText && (adding = null)} />
				</form>
			{:else}
				<button class="add-btn" onclick={() => (adding = c.id)}><Plus size={15} /> Add task</button>
			{/if}
		</section>
	{/each}
	<section class="col new">
		<form onsubmit={(e) => { e.preventDefault(); addColumn(); }}>
			<input class="input" bind:value={newCol} placeholder="+ New column" aria-label="New column name" />
		</form>
		<label class="old"><input type="checkbox" bind:checked={showOldDone} /> Show done older than 14 days</label>
	</section>
</div>

<style>
	.board {
		display: flex;
		gap: 14px;
		align-items: flex-start;
		overflow-x: auto;
		padding-bottom: 16px;
		scroll-snap-type: x mandatory;
		min-height: 60vh;
	}
	.col {
		flex: 0 0 300px;
		max-width: 300px;
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 10px;
		border-radius: var(--r);
		background: var(--sidebar);
		border: 1px solid var(--border);
		scroll-snap-align: start;
		transition: border-color var(--dur) var(--ease), background var(--dur) var(--ease);
	}
	.col.drop {
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	header {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 32px;
	}
	.name {
		flex: 1;
		text-align: left;
		border: 0;
		background: none;
		font-weight: 600;
		padding: 0;
	}
	.rename {
		flex: 1;
		min-height: 30px;
		padding: 4px 8px;
	}
	.count {
		font-size: 12px;
		color: var(--muted);
	}
	.count.over {
		color: var(--p4);
		font-weight: 700;
	}
	.sd {
		width: 9px;
		height: 9px;
		border-radius: 99px;
		border: 1.5px solid var(--faint);
	}
	.s-doing,
	.s-waiting {
		background: var(--accent);
		border-color: var(--accent);
	}
	.s-done {
		background: var(--done);
		border-color: var(--done);
	}
	.s-cancelled {
		background: var(--faint);
	}
	header :global(.icon) {
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
	}
	.cards {
		display: flex;
		flex-direction: column;
		gap: 6px;
		min-height: 8px;
	}
	/* Cards: checkbox | title | ⋯ on one row, meta underneath the title. */
	.card :global(.row) {
		display: grid;
		grid-template-columns: auto 1fr auto;
		align-items: start;
		column-gap: 10px;
		row-gap: 4px;
		min-height: 0;
		padding: 10px 6px 10px 10px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--r-sm);
	}
	.card :global(.row .check) {
		margin-top: 1px;
	}
	.card :global(.row .meta) {
		grid-column: 2 / 4;
		grid-row: 2;
		padding-left: 0;
	}
	.card :global(.row .meta:empty) {
		display: none;
	}
	.card :global(.row .more) {
		grid-column: 3;
		grid-row: 1;
		width: 24px;
		height: 24px;
	}
	.card.dragging {
		opacity: 0.4;
	}
	.add-btn {
		display: flex;
		align-items: center;
		gap: 6px;
		min-height: 34px;
		padding: 0 8px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
	}
	.add-btn:hover {
		background: var(--hover);
		color: var(--text);
	}
	.new {
		background: transparent;
		border-style: dashed;
	}
	.old {
		display: flex;
		gap: 6px;
		align-items: center;
		font-size: 12px;
		color: var(--muted);
	}
	@media (max-width: 640px) {
		.col {
			flex-basis: 86vw;
			max-width: 86vw;
		}
	}
</style>
