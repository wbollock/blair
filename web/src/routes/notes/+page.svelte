<script lang="ts">
	import { page } from '$app/state';
	import Pin from '@lucide/svelte/icons/pin';
	import { api, ok, uuidv7, type Note } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import { relativeTime } from '#lib/dates.ts';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import Markdown from '#lib/components/Markdown.svelte';

	let notes = $state<Note[]>([]);
	let draft = $state('');
	let editing = $state<Note | null>(null);
	let editBody = $state('');
	let editTitle = $state('');

	async function load() {
		notes = (await ok(api.GET('/notes'))).data;
		const open = page.url.searchParams.get('open');
		if (open) {
			const n = notes.find((x) => x.id === open);
			if (n) edit(n);
		}
	}
	$effect(() => {
		load();
	});
	async function create() {
		if (!draft.trim()) return;
		const n = await ok(api.POST('/notes', { body: { id: uuidv7(), body: draft } }));
		draft = '';
		notes = [n, ...notes];
	}
	function edit(n: Note) {
		editing = n;
		editBody = n.body;
		editTitle = n.title;
	}
	async function save() {
		if (!editing) return;
		const n = await ok(api.PATCH('/notes/{id}', { params: { path: { id: editing.id } }, body: { title: editTitle, body: editBody } }));
		notes = notes.map((x) => (x.id === n.id ? n : x));
		editing = null;
	}
	async function pin(n: Note) {
		const u = await ok(api.PATCH('/notes/{id}', { params: { path: { id: n.id } }, body: { pinned: !n.pinned } }));
		notes = notes.map((x) => (x.id === u.id ? u : x)).sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.updated_at.localeCompare(a.updated_at));
	}
	async function convert(n: Note) {
		const t = await ok(api.POST('/notes/{id}/convert', { params: { path: { id: n.id } }, body: { delete_note: true } }));
		app.upsert(t);
		app.justAdded = [t.id, ...app.justAdded].slice(0, 8);
		notes = notes.filter((x) => x.id !== n.id);
		editing = null;
		app.toast('Turned into a task', 'info', [{ label: 'Open', run: () => (app.drawer = t.id) }]);
	}
	async function remove(n: Note) {
		await ok(api.DELETE('/notes/{id}', { params: { path: { id: n.id } } }));
		notes = notes.filter((x) => x.id !== n.id);
		editing = null;
		app.toast('Note moved to trash', 'info', [
			{ label: 'Undo', run: async () => { const r = await ok(api.POST('/notes/{id}/restore', { params: { path: { id: n.id } } })); notes = [r, ...notes]; } }
		]);
	}
</script>

<svelte:head><title>Notes · Blair</title></svelte:head>
<ViewHeader title="Notes" subtitle="Quick thoughts that aren't tasks (yet)" />

<div class="new">
	<textarea class="input" bind:value={draft} placeholder="Jot something down… markdown works" onkeydown={(e) => (e.metaKey || e.ctrlKey) && e.key === 'Enter' && create()} aria-label="New note"></textarea>
	<button class="btn btn-primary" disabled={!draft.trim()} onclick={create}>Save note</button>
</div>

<div class="grid">
	{#each notes as n (n.id)}
		<article class="note" class:pinned={n.pinned}>
			{#if editing?.id === n.id}
				<input class="input title" bind:value={editTitle} placeholder="Title (optional)" aria-label="Note title" />
				<textarea class="input body" bind:value={editBody} aria-label="Note body"></textarea>
				<div class="actions">
					<button class="btn btn-primary" onclick={save}>Save</button>
					<button class="btn" onclick={() => (editing = null)}>Cancel</button>
				</div>
			{:else}
				<button class="view" onclick={() => edit(n)}>
					{#if n.title}<h3>{n.title}</h3>{/if}
					<Markdown src={n.body} />
				</button>
				<footer>
					<span class="muted">{relativeTime(n.updated_at)}</span>
					<button class="icon" class:on={n.pinned} aria-label={n.pinned ? 'Unpin' : 'Pin'} aria-pressed={n.pinned} onclick={() => pin(n)}><Pin size={14} /></button>
					<button class="linkish" onclick={() => convert(n)}>→ Task</button>
					<button class="linkish danger" onclick={() => remove(n)}>Delete</button>
				</footer>
			{/if}
		</article>
	{:else}
		<p class="muted">No notes yet.</p>
	{/each}
</div>

<style>
	.new {
		display: flex;
		flex-direction: column;
		gap: 8px;
		max-width: 860px;
		margin-bottom: 22px;
		align-items: flex-end;
	}
	.new textarea {
		min-height: 90px;
		resize: vertical;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
		gap: 14px;
		align-items: start;
	}
	.note {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px;
		border-radius: var(--r);
		background: var(--surface);
		border: 1px solid var(--border);
	}
	.note.pinned {
		border-color: var(--accent);
	}
	.view {
		text-align: left;
		border: 0;
		background: none;
		padding: 0;
		max-height: 320px;
		overflow: hidden;
	}
	h3 {
		margin: 0 0 6px;
		font-family: var(--font-display);
		font-size: 17px;
	}
	.title {
		font-weight: 600;
	}
	.body {
		min-height: 200px;
		resize: vertical;
	}
	.actions,
	footer {
		display: flex;
		gap: 10px;
		align-items: center;
	}
	footer .muted {
		flex: 1;
	}
	.icon {
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--faint);
	}
	.icon.on {
		color: var(--accent);
	}
	.linkish {
		border: 0;
		background: none;
		font-size: 12.5px;
		color: var(--accent-ink);
		font-weight: 600;
	}
	.linkish.danger {
		color: var(--danger);
	}
	.muted {
		color: var(--muted);
		font-size: 12.5px;
	}
</style>
