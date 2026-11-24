<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ok, type Schemas } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import ViewHeader from '#lib/components/ViewHeader.svelte';
	import TaskRow from '#lib/components/TaskRow.svelte';

	let q = $state(page.url.searchParams.get('q') ?? '');
	let hits = $state<Schemas['SearchHit'][]>([]);
	let timer: ReturnType<typeof setTimeout>;
	let input: HTMLInputElement;

	$effect(() => {
		input.focus();
	});
	$effect(() => {
		const query = q.trim();
		clearTimeout(timer);
		timer = setTimeout(async () => {
			if (!query) {
				hits = [];
				return;
			}
			hits = (await ok(api.GET('/search', { params: { query: { q: query } } }))).data;
			await Promise.all(hits.filter((h) => h.kind === 'task' && !app.tasks[h.id]).map((h) => app.refreshTask(h.id)));
			history.replaceState(history.state, '', `/search?q=${encodeURIComponent(query)}`);
		}, 180);
	});
</script>

<svelte:head><title>Search · Blair</title></svelte:head>
<ViewHeader title="Search" />
<div class="wrap">
	<input bind:this={input} class="input big" type="search" placeholder="Search tasks and notes…" bind:value={q} aria-label="Search" />
	<div class="results" role="list">
		{#each hits as h (h.kind + h.id)}
			{#if h.kind === 'task' && app.tasks[h.id]}
				<TaskRow task={app.tasks[h.id]} />
			{:else if h.kind === 'note'}
				<button class="note" onclick={() => goto(`/notes?open=${h.id}`)}>
					<span class="kind">Note</span>
					<b>{h.title || 'Untitled note'}</b>
					<span class="snip">{h.snippet}</span>
				</button>
			{/if}
		{:else}
			{#if q.trim()}<p class="muted">No matches.</p>{/if}
		{/each}
	</div>
</div>

<style>
	.wrap {
		max-width: 860px;
	}
	.big {
		min-height: 48px;
		font-size: 16px;
		margin-bottom: 14px;
	}
	.note {
		display: flex;
		gap: 10px;
		align-items: baseline;
		width: 100%;
		text-align: left;
		padding: 12px 8px;
		border: 0;
		border-bottom: 1px solid var(--border);
		background: none;
		flex-wrap: wrap;
	}
	.kind {
		font-size: 11px;
		padding: 1px 6px;
		border-radius: 4px;
		background: var(--chip);
		color: var(--muted);
	}
	.snip {
		color: var(--muted);
		font-size: 13px;
	}
	.muted {
		color: var(--muted);
	}
</style>
