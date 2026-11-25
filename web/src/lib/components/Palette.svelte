<script lang="ts" module>
	export const palette = $state({ open: false });
</script>

<script lang="ts">
	import { goto } from '$app/navigation';
	import { app, isOpen } from '#lib/state.svelte.ts';
	import { rank } from '#lib/fuzzy.ts';
	import { focusQuickAdd } from './QuickAdd.svelte';

	interface Item {
		kind: 'Go to' | 'Project' | 'Label' | 'Task' | 'Action';
		label: string;
		hint?: string;
		run: () => void;
	}

	let query = $state('');
	let active = $state(0);
	let input: HTMLInputElement | undefined = $state();
	let list: HTMLElement | undefined = $state();

	$effect(() => {
		if (!palette.open) return;
		query = '';
		active = 0;
		queueMicrotask(() => input?.focus());
		// Fill the store with every open task so search covers more than the current view.
		app.loadTasks({ status: 'todo,doing,waiting' }).catch(() => {});
	});

	const go = (href: string) => () => goto(href);
	const views: Item[] = [
		['Inbox', '/inbox'], ['Today', '/today'], ['Upcoming', '/upcoming'], ['Weekly review', '/review'], ['Notes', '/notes'],
		['Search (full text)', '/search'], ['Triage inbox', '/triage'], ['Recently added', '/recent'], ['Logbook', '/logbook'], ['Trash', '/trash'], ['Settings', '/settings']
	].map(([label, href]) => ({ kind: 'Go to', label, run: go(href) }));
	const actions: Item[] = [
		{ kind: 'Action', label: 'New task', run: () => (matchMedia('(max-width: 640px)').matches ? (app.captureOpen = true) : focusQuickAdd()) },
		{ kind: 'Action', label: 'Theme: light', run: () => app.saveSettings({ theme: 'light' }) },
		{ kind: 'Action', label: 'Theme: dark', run: () => app.saveSettings({ theme: 'dark' }) },
		{ kind: 'Action', label: 'Theme: follow system', run: () => app.saveSettings({ theme: 'system' }) }
	];
	const plain = (s: string) => s.replace(/[*_`~]/g, '');

	const items = $derived.by((): Item[] => {
		const projects = app.projects.filter((p) => !p.archived_at).map((p): Item => ({ kind: 'Project', label: p.name, run: go(`/project/${p.id}`) }));
		const labels = app.labels.map((l): Item => ({ kind: 'Label', label: '@' + l.name, run: go(`/label/${l.id}`) }));
		const tasks = Object.values(app.tasks)
			.filter((t) => !t.deleted_at)
			.sort((a, b) => Number(isOpen(b)) - Number(isOpen(a)))
			.map((t): Item => ({
				kind: 'Task',
				label: plain(t.title),
				hint: [app.project(t.project_id)?.name ?? 'Inbox', isOpen(t) ? '' : t.status].filter(Boolean).join(' · '),
				run: () => (app.drawer = t.id)
			}));
		// With no query, show navigation; tasks appear once you type.
		const nav = app.planEnabled ? [{ kind: 'Go to', label: 'Plan today', run: go('/plan') } as Item, ...views] : views;
		return query.trim() ? [...nav, ...projects, ...labels, ...actions, ...tasks] : [...nav, ...projects, ...actions];
	});
	const results = $derived(rank(query, items, (i) => i.label, 40));
	$effect(() => {
		void results;
		active = 0;
	});

	function run(i: number) {
		const r = results[i];
		if (!r) return;
		palette.open = false;
		r.item.run();
	}
	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault();
			const n = results.length || 1;
			active = (active + (e.key === 'ArrowDown' ? 1 : -1) + n) % n;
			list?.querySelector(`[data-i="${active}"]`)?.scrollIntoView({ block: 'nearest' });
		} else if (e.key === 'Enter') {
			e.preventDefault();
			run(active);
		} else if (e.key === 'Escape') {
			e.preventDefault();
			palette.open = false;
		}
	}
	/** Splits a label into runs for highlighting the matched characters. */
	function parts(label: string, hits: number[]) {
		const set = new Set(hits);
		const out: { text: string; hit: boolean }[] = [];
		for (let i = 0; i < label.length; i++) {
			const hit = set.has(i);
			if (out.length && out[out.length - 1].hit === hit) out[out.length - 1].text += label[i];
			else out.push({ text: label[i], hit });
		}
		return out;
	}
</script>

{#if palette.open}
	<div class="scrim" onclick={() => (palette.open = false)} aria-hidden="true"></div>
	<div class="palette" role="dialog" aria-modal="true" aria-label="Command palette">
		<input
			bind:this={input}
			bind:value={query}
			{onkeydown}
			class="q"
			placeholder="Jump to a task, project or view…"
			aria-label="Search tasks, projects, views and actions"
			role="combobox"
			aria-expanded="true"
			aria-controls="palette-list"
			aria-activedescendant={results.length ? `palette-${active}` : undefined}
			autocomplete="off"
			spellcheck="false"
		/>
		<div class="list" id="palette-list" role="listbox" bind:this={list}>
			{#each results as r, i (i)}
				<div
					class="opt"
					class:on={i === active}
					id="palette-{i}"
					data-i={i}
					role="option"
					aria-selected={i === active}
					tabindex="-1"
					onclick={() => run(i)}
					onkeydown={() => {}}
					onpointermove={() => (active = i)}
				>
					<span class="kind">{r.item.kind}</span>
					<span class="label">{#each parts(r.item.label, r.hits) as p, k (k)}{#if p.hit}<b>{p.text}</b>{:else}{p.text}{/if}{/each}</span>
					{#if r.item.hint}<span class="hint">{r.item.hint}</span>{/if}
				</div>
			{:else}
				<p class="empty">No matches. Press <kbd>/</kbd> to search task notes too.</p>
			{/each}
		</div>
	</div>
{/if}

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: var(--scrim);
	}
	.palette {
		position: fixed;
		z-index: 71;
		top: max(12vh, calc(16px + env(safe-area-inset-top)));
		left: 50%;
		transform: translateX(-50%);
		width: min(600px, calc(100vw - 32px));
		display: flex;
		flex-direction: column;
		border: 1px solid var(--border);
		border-radius: var(--r);
		background: var(--menu);
		box-shadow: var(--shadow-float);
		overflow: hidden;
	}
	.q {
		border: 0;
		border-bottom: 1px solid var(--border);
		padding: 14px 16px;
		font: inherit;
		font-size: 16px;
		background: transparent;
		color: var(--text);
		outline: none;
	}
	.list {
		max-height: min(420px, 55dvh);
		overflow-y: auto;
		padding: 6px;
	}
	.opt {
		display: flex;
		align-items: baseline;
		gap: 10px;
		padding: 8px 10px;
		border-radius: var(--r-sm);
		cursor: pointer;
		font-size: 14px;
	}
	.opt.on {
		background: var(--selected);
	}
	.kind {
		flex: none;
		width: 56px;
		font-size: 11px;
		color: var(--faint);
	}
	.label {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.label b {
		color: var(--accent-ink);
		font-weight: 700;
	}
	.hint {
		flex: none;
		max-width: 40%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: 12px;
		color: var(--muted);
	}
	.empty {
		margin: 0;
		padding: 14px 10px;
		font-size: 13px;
		color: var(--muted);
	}
</style>
