<script lang="ts">
	import X from '@lucide/svelte/icons/x';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import { api, ok, type ChecklistItem, type Status } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import { relativeTime } from '#lib/dates.ts';
	import Checkbox from './Checkbox.svelte';
	import Markdown from './Markdown.svelte';

	const task = $derived(app.drawer ? app.tasks[app.drawer] : undefined);
	let checklist = $state<ChecklistItem[]>([]);
	let title = $state('');
	let desc = $state('');
	let editingDesc = $state(false);
	let newItem = $state('');
	let loadedFor = '';
	let panel: HTMLElement | undefined = $state();

	$effect(() => {
		const id = app.drawer;
		if (!id) return;
		if (!app.tasks[id]) app.refreshTask(id);
		if (loadedFor !== id) {
			loadedFor = id;
			editingDesc = false;
			loadChecklist(id);
			queueMicrotask(() => panel?.querySelector<HTMLElement>('.title-input')?.focus());
		}
	});
	$effect(() => {
		if (task) {
			title = task.title;
			if (!editingDesc) desc = task.description;
		}
	});

	async function loadChecklist(id: string) {
		checklist = [];
		if (!app.tasks[id] || app.tasks[id].version === 0) return;
		try {
			checklist = (await ok(api.GET('/tasks/{id}/checklist', { params: { path: { id } } }))).data;
		} catch {
			/* task may be gone */
		}
	}
	function close() {
		app.drawer = null;
		loadedFor = '';
	}
	function saveTitle() {
		if (task && title.trim() && title.trim() !== task.title) app.update(task.id, { title: title.trim() });
		else if (task) title = task.title;
	}
	function saveDesc() {
		editingDesc = false;
		if (task && desc !== task.description) app.update(task.id, { description: desc });
	}
	async function addItem() {
		if (!task || !newItem.trim()) return;
		const text = newItem.trim();
		newItem = '';
		try {
			checklist = [...checklist, await ok(api.POST('/tasks/{id}/checklist', { params: { path: { id: task.id } }, body: { text } }))];
			app.refreshTask(task.id);
		} catch (e) {
			app.toast(e instanceof Error ? e.message : 'Could not add item', 'error');
		}
	}
	async function toggleItem(c: ChecklistItem) {
		const updated = await ok(api.PATCH('/checklist/{id}', { params: { path: { id: c.id } }, body: { done: !c.done } }));
		checklist = checklist.map((x) => (x.id === c.id ? updated : x));
		if (task) app.refreshTask(task.id);
	}
	async function deleteItem(c: ChecklistItem) {
		await ok(api.DELETE('/checklist/{id}', { params: { path: { id: c.id } } }));
		checklist = checklist.filter((x) => x.id !== c.id);
		if (task) app.refreshTask(task.id);
	}
	const statuses: Status[] = ['todo', 'doing', 'waiting', 'done', 'cancelled'];
	const priorities = ['None', 'Low', 'Medium', 'High', 'Urgent'];
	const project = $derived(app.project(task?.project_id));
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && app.drawer && !app.quick && close()} />

{#if app.drawer}
	<div class="scrim" onclick={close} aria-hidden="true"></div>
	<aside class="drawer" bind:this={panel} aria-label="Task details">
		{#if task}
			<header>
				<Checkbox done={task.status === 'done' || task.status === 'cancelled'} priority={task.priority} size={22} onclick={() => app.toggleDone(task.id)} />
				<input class="title-input" bind:value={title} onblur={saveTitle} onkeydown={(e) => e.key === 'Enter' && (e.currentTarget as HTMLInputElement).blur()} aria-label="Title" maxlength="500" />
				<button class="icon" onclick={close} aria-label="Close"><X size={18} /></button>
			</header>
			{#if /[*_`~[]/.test(task.title)}
				<div class="preview"><Markdown src={task.title} inline /></div>
			{/if}

			<div class="grid">
				<label for="d-status">Status</label>
				<select id="d-status" class="input" value={task.status} onchange={(e) => app.update(task.id, { status: (e.currentTarget as HTMLSelectElement).value as Status })}>
					{#each statuses as s (s)}<option value={s}>{s}</option>{/each}
				</select>

				<label for="d-project">Project</label>
				<select id="d-project" class="input" value={task.project_id ?? ''} onchange={(e) => app.update(task.id, { project_id: (e.currentTarget as HTMLSelectElement).value || null })}>
					<option value="">Inbox</option>
					{#each app.projects.filter((p) => !p.archived_at || p.id === task.project_id) as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
				</select>

				{#if project}
					<label for="d-column">Column</label>
					<select id="d-column" class="input" value={task.column_id ?? ''} onchange={(e) => app.update(task.id, { column_id: (e.currentTarget as HTMLSelectElement).value })}>
						{#each project.columns as c (c.id)}<option value={c.id}>{c.name} ({c.status})</option>{/each}
					</select>
				{/if}

				<label for="d-due">Due</label>
				<div class="due">
					<input id="d-due" type="date" class="input" value={task.due_date ?? ''} onchange={(e) => app.update(task.id, { due_date: (e.currentTarget as HTMLInputElement).value || null })} />
					<input type="time" class="input" aria-label="Due time" disabled={!task.due_date} value={task.due_time ?? ''} onchange={(e) => app.update(task.id, { due_time: (e.currentTarget as HTMLInputElement).value || null })} />
				</div>

				<span class="lbl">Priority</span>
				<div class="seg" role="radiogroup" aria-label="Priority">
					{#each priorities as name, p (name)}
						<button role="radio" aria-checked={task.priority === p} class="p{p}" class:on={task.priority === p} onclick={() => app.update(task.id, { priority: p })}>{name}</button>
					{/each}
				</div>

				<span class="lbl">Labels</span>
				<div class="labels">
					{#each app.labels as l (l.id)}
						{@const on = task.labels.includes(l.id)}
						<button class="chip" class:set={on} aria-pressed={on} onclick={() => app.update(task.id, { labels: on ? task.labels.filter((x) => x !== l.id) : [...task.labels, l.id] })}>@{l.name}</button>
					{/each}
					{#if !app.labels.length}<span class="muted">Type @label in quick-add to create labels</span>{/if}
				</div>
			</div>

			<section>
				<div class="sec-head"><span class="eyebrow">Notes</span>
					<button class="linkish" onclick={() => (editingDesc ? saveDesc() : (editingDesc = true))}>{editingDesc ? 'Done' : 'Edit'}</button>
				</div>
				{#if editingDesc}
					<!-- svelte-ignore a11y_autofocus -->
					<textarea class="input desc" bind:value={desc} onblur={saveDesc} autofocus placeholder="Markdown supported: **bold**, lists, - [ ] tasks, `code`…"></textarea>
				{:else if task.description}
					<button class="desc-view" onclick={() => (editingDesc = true)}><Markdown src={task.description} /></button>
				{:else}
					<button class="desc-empty" onclick={() => (editingDesc = true)}>Add notes…</button>
				{/if}
			</section>

			<section>
				<div class="sec-head"><span class="eyebrow">Checklist {checklist.length ? `${checklist.filter((c) => c.done).length}/${checklist.length}` : ''}</span></div>
				{#each checklist as c (c.id)}
					<div class="ci">
						<Checkbox done={c.done} size={16} label="Tick item" onclick={() => toggleItem(c)} />
						<span class:done={c.done}><Markdown src={c.text} inline /></span>
						<button class="icon sm" aria-label="Delete item" onclick={() => deleteItem(c)}><X size={14} /></button>
					</div>
				{/each}
				<form class="add-item" onsubmit={(e) => { e.preventDefault(); addItem(); }}>
					<input class="input" bind:value={newItem} placeholder="Add an item…" aria-label="New checklist item" />
				</form>
			</section>

			<footer>
				<span class="muted">Created {relativeTime(task.created_at)}{task.completed_at ? ` · completed ${relativeTime(task.completed_at)}` : ''}</span>
				<button class="btn btn-ghost btn-danger" onclick={() => app.remove(task.id)}><Trash2 size={15} /> Delete</button>
			</footer>
		{:else}
			<p class="muted">Loading…</p>
		{/if}
	</aside>
{/if}

<style>
	.scrim {
		position: fixed;
		inset: 0;
		background: var(--scrim);
		z-index: 40;
		animation: fade 160ms var(--ease);
	}
	.drawer {
		position: fixed;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(480px, 100%);
		z-index: 45;
		overflow-y: auto;
		padding: 20px 22px calc(24px + env(safe-area-inset-bottom));
		background: var(--bg);
		border-left: 1px solid var(--border);
		box-shadow: var(--shadow-float);
		display: flex;
		flex-direction: column;
		gap: 18px;
		animation: slide 200ms var(--ease);
	}
	@keyframes slide {
		from {
			transform: translateX(24px);
			opacity: 0;
		}
	}
	@keyframes fade {
		from {
			opacity: 0;
		}
	}
	header {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.title-input {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		font-family: var(--font-display);
		font-size: 22px;
		font-weight: 600;
		padding: 4px 0;
		outline: none;
		border-bottom: 1.5px solid transparent;
	}
	.title-input:focus {
		border-bottom-color: var(--accent);
	}
	.preview {
		margin-top: -10px;
		padding-left: 34px;
		color: var(--muted);
	}
	.icon {
		display: grid;
		place-items: center;
		width: 34px;
		height: 34px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
	}
	.icon:hover {
		background: var(--chip);
	}
	.icon.sm {
		width: 26px;
		height: 26px;
	}
	.grid {
		display: grid;
		grid-template-columns: 90px 1fr;
		gap: 10px 14px;
		align-items: center;
	}
	.grid label,
	.lbl {
		font-size: 12.5px;
		font-weight: 600;
		color: var(--muted);
	}
	.due {
		display: flex;
		gap: 8px;
	}
	.due .input[type='time'] {
		max-width: 130px;
	}
	.seg {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
	}
	.seg button {
		min-height: 32px;
		padding: 0 10px;
		border-radius: var(--r-sm);
		border: 1px solid var(--border);
		background: var(--surface);
		font-size: 12.5px;
	}
	.seg button.on {
		border-color: var(--text);
		font-weight: 600;
	}
	.seg .p1.on { background: var(--p1-soft); border-color: var(--p1); }
	.seg .p2.on { background: var(--p2-soft); border-color: var(--p2); }
	.seg .p3.on { background: var(--p3-soft); border-color: var(--p3); }
	.seg .p4.on { background: var(--p4-soft); border-color: var(--p4); }
	.labels {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.sec-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 8px;
	}
	.linkish {
		border: 0;
		background: none;
		color: var(--accent-ink);
		font-weight: 600;
		font-size: 12.5px;
	}
	.desc {
		min-height: 180px;
		resize: vertical;
		font-family: var(--font-ui);
		line-height: 1.5;
	}
	.desc-view,
	.desc-empty {
		display: block;
		width: 100%;
		text-align: left;
		padding: 10px 12px;
		border-radius: var(--r-sm);
		border: 1px solid transparent;
		background: var(--surface);
	}
	.desc-view:hover,
	.desc-empty:hover {
		border-color: var(--border);
	}
	.desc-empty {
		color: var(--faint);
	}
	.ci {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 36px;
	}
	.ci span {
		flex: 1;
	}
	.ci .done {
		color: var(--muted);
		text-decoration: line-through;
	}
	.add-item {
		margin-top: 6px;
	}
	footer {
		margin-top: auto;
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 10px;
		padding-top: 12px;
		border-top: 1px solid var(--border);
	}
	.muted {
		color: var(--muted);
		font-size: 12.5px;
	}
	@media (max-width: 640px) {
		.drawer {
			width: 100%;
			border-left: 0;
			padding-top: calc(16px + env(safe-area-inset-top));
		}
		.grid {
			grid-template-columns: 1fr;
			gap: 6px;
		}
		.grid > :global(label),
		.grid > .lbl {
			margin-top: 6px;
		}
	}
</style>
