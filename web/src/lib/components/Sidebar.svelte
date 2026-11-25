<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import Inbox from '@lucide/svelte/icons/inbox';
	import Sun from '@lucide/svelte/icons/sun';
	import CalendarDays from '@lucide/svelte/icons/calendar-days';
	import StickyNote from '@lucide/svelte/icons/sticky-note';
	import Search from '@lucide/svelte/icons/search';
	import History from '@lucide/svelte/icons/history';
	import Archive from '@lucide/svelte/icons/archive';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import Settings from '@lucide/svelte/icons/settings';
	import Sparkles from '@lucide/svelte/icons/sparkles';
	import ClipboardCheck from '@lucide/svelte/icons/clipboard-check';
	import Plus from '@lucide/svelte/icons/plus';
	import { api, ok } from '#lib/api.ts';
	import { app, isOpen } from '#lib/state.svelte.ts';
	import Notifications from './Notifications.svelte';

	let newProject = $state('');
	let creating = $state(false);
	const path = $derived(page.url.pathname);
	const todayCount = $derived(Object.values(app.tasks).filter((t) => isOpen(t) && t.due_date && t.due_date <= app.today).length);
	const nav = $derived([
		{ href: '/inbox', label: 'Inbox', icon: Inbox, count: app.inboxCount },
		{ href: '/today', label: 'Today', icon: Sun, count: todayCount },
		{ href: '/upcoming', label: 'Upcoming', icon: CalendarDays },
		{ href: '/notes', label: 'Notes', icon: StickyNote },
		{ href: '/search', label: 'Search', icon: Search },
		{ href: '/review', label: 'Weekly review', icon: ClipboardCheck }
	]);
	const projects = $derived(app.projects.filter((p) => !p.archived_at));
	const palette = ['#2E9E6A', '#5E6AD2', '#C2410C', '#B83280', '#0E7490', '#A16207', '#7C3AED', '#BE123C'];

	async function create() {
		const name = newProject.trim();
		if (!name) return;
		newProject = '';
		creating = false;
		try {
			const p = await ok(api.POST('/projects', { body: { name, color: palette[app.projects.length % palette.length] } }));
			await app.loadProjects();
			app.sidebarOpen = false;
			goto(`/project/${p.id}`);
		} catch (e) {
			app.toast(e instanceof Error ? e.message : 'Could not create project', 'error');
		}
	}
	const close = () => (app.sidebarOpen = false);

	// Drop a dragged task on Inbox or a project to file it.
	let dropTarget = $state<string | null>(null);
	function over(e: DragEvent, target: string) {
		if (!app.dragId) return;
		e.preventDefault();
		dropTarget = target;
	}
	function drop(e: DragEvent, projectId: string | null) {
		e.preventDefault();
		const id = app.dragId;
		app.dragId = dropTarget = null;
		const t = id ? app.tasks[id] : undefined;
		if (!t || t.project_id === projectId) return;
		const name = projectId ? (app.project(projectId)?.name ?? 'project') : 'Inbox';
		app.update(t.id, { project_id: projectId }, { undo: `Moved to ${name}` });
	}
</script>

<nav class="sidebar" class:open={app.sidebarOpen} aria-label="Main">
	<div class="brand">
		<a href="/today" onclick={close}>Blair</a>
		<span class="desktop-only"><Notifications /></span>
	</div>
	{#each nav as n (n.href)}
		<a
			class="item"
			class:active={path === n.href}
			class:drop={n.href === '/inbox' && dropTarget === 'inbox'}
			aria-current={path === n.href ? 'page' : undefined}
			href={n.href}
			onclick={close}
			ondragover={n.href === '/inbox' ? (e) => over(e, 'inbox') : undefined}
			ondragleave={() => (dropTarget = null)}
			ondrop={n.href === '/inbox' ? (e) => drop(e, null) : undefined}
		>
			<n.icon size={17} aria-hidden="true" />
			<span class="grow">{n.label}</span>
			{#if n.count}<span class="count" class:accent={n.href === '/inbox'}>{n.count}</span>{/if}
		</a>
	{/each}
	{#if app.inboxCount}
		<a class="item sub" href="/triage" onclick={close} class:active={path === '/triage'}><Sparkles size={15} aria-hidden="true" /><span class="grow">Triage inbox</span></a>
	{/if}

	<div class="section">
		<span class="eyebrow">Projects</span>
		<button class="icon" aria-label="New project" onclick={() => (creating = !creating)}><Plus size={15} /></button>
	</div>
	{#if creating}
		<form onsubmit={(e) => { e.preventDefault(); create(); }}>
			<!-- svelte-ignore a11y_autofocus -->
			<input class="input" bind:value={newProject} autofocus placeholder="Project name" aria-label="Project name" onblur={() => !newProject && (creating = false)} onkeydown={(e) => e.key === 'Escape' && (creating = false)} />
		</form>
	{/if}
	{#each projects as p (p.id)}
		<a
			class="item"
			class:active={path === `/project/${p.id}`}
			class:drop={dropTarget === p.id}
			href="/project/{p.id}"
			onclick={close}
			ondragover={(e) => over(e, p.id)}
			ondragleave={() => (dropTarget = null)}
			ondrop={(e) => drop(e, p.id)}
		>
			<span class="dot" style="background: {p.color || 'var(--accent)'}"></span>
			<span class="grow">{p.name}</span>
			{#if p.open_tasks}<span class="count">{p.open_tasks}</span>{/if}
		</a>
	{/each}
	{#if !projects.length && !creating}
		<button class="item ghost" onclick={() => (creating = true)}>+ Create your first project</button>
	{/if}

	{#if app.labels.length}
		<div class="section"><span class="eyebrow">Labels</span></div>
		<div class="labels">
			{#each app.labels as l (l.id)}
				<a class="lab" class:active={path === `/label/${l.id}`} href="/label/{l.id}" onclick={close}>@{l.name}</a>
			{/each}
		</div>
	{/if}

	<div class="grow"></div>
	<div class="foot">
		<a class="item" href="/recent" class:active={path === '/recent'} onclick={close}><History size={16} aria-hidden="true" /><span class="grow">Recently added</span></a>
		<a class="item" href="/logbook" class:active={path === '/logbook'} onclick={close}><Archive size={16} aria-hidden="true" /><span class="grow">Logbook</span></a>
		<a class="item" href="/trash" class:active={path === '/trash'} onclick={close}><Trash2 size={16} aria-hidden="true" /><span class="grow">Trash</span></a>
		<a class="item" href="/settings" class:active={path.startsWith('/settings')} onclick={close}><Settings size={16} aria-hidden="true" /><span class="grow">Settings</span></a>
		<div class="me">
			<span class="avatar">{(app.me?.display_name || app.me?.email || '?')[0].toUpperCase()}</span>
			<span class="who">{app.me?.display_name || app.me?.email}</span>
		</div>
	</div>
</nav>
{#if app.sidebarOpen}
	<div class="scrim" onclick={close} aria-hidden="true"></div>
{/if}

<style>
	.sidebar {
		position: sticky;
		top: 0;
		height: 100dvh;
		width: var(--sidebar-w);
		flex-shrink: 0;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 18px 12px 14px;
		background: var(--sidebar);
		border-right: 1px solid var(--border);
	}
	.brand {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0 4px 14px 10px;
	}
	@media (max-width: 1023px) {
		.desktop-only {
			display: none;
		}
	}
	.brand a {
		font-family: var(--font-display);
		font-size: 24px;
		font-weight: 600;
		color: var(--text);
		letter-spacing: -0.01em;
		text-decoration: none;
	}
	.item {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 36px;
		padding: 0 10px;
		border-radius: var(--r-sm);
		color: var(--text);
		text-decoration: none;
		border: 0;
		background: none;
		text-align: left;
		width: 100%;
	}
	.item:hover {
		background: var(--hover);
		text-decoration: none;
	}
	.item.active {
		background: var(--selected);
		font-weight: 600;
	}
	.item.drop {
		background: var(--accent-soft);
		box-shadow: inset 0 0 0 1.5px var(--accent);
	}
	.item.sub {
		min-height: 32px;
		padding-left: 22px;
		font-size: 13px;
		color: var(--accent-ink);
	}
	.item.ghost {
		color: var(--muted);
		font-size: 13px;
	}
	.item :global(svg) {
		color: var(--muted);
	}
	.grow {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.count {
		font-size: 12px;
		color: var(--muted);
	}
	.count.accent {
		color: var(--accent-ink);
		font-weight: 700;
	}
	.section {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 20px 10px 6px;
	}
	.icon {
		display: grid;
		place-items: center;
		width: 26px;
		height: 26px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
	}
	.icon:hover {
		background: var(--chip);
	}
	form {
		padding: 2px 4px 6px;
	}
	form .input {
		min-height: 34px;
	}
	.labels {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		padding: 0 8px;
	}
	.lab {
		font-size: 12.5px;
		padding: 2px 8px;
		border-radius: var(--r-sm);
		color: var(--muted);
	}
	.lab:hover,
	.lab.active {
		background: var(--selected);
		color: var(--text);
		text-decoration: none;
	}
	.foot {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding-top: 12px;
		border-top: 1px solid var(--border);
	}
	.foot .item {
		min-height: 32px;
		font-size: 13px;
	}
	.me {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 10px 0;
		font-size: 13px;
		color: var(--muted);
	}
	.avatar {
		width: 26px;
		height: 26px;
		display: grid;
		place-items: center;
		border-radius: 99px;
		background: var(--accent-soft);
		color: var(--accent-ink);
		font-weight: 700;
		font-size: 12px;
	}
	.who {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.scrim {
		display: none;
	}
	@media (max-width: 1023px) {
		.sidebar {
			position: fixed;
			left: 0;
			z-index: 60;
			transform: translateX(-100%);
			transition: transform 220ms var(--ease);
			box-shadow: var(--shadow-float);
			padding-top: calc(18px + env(safe-area-inset-top));
		}
		.sidebar.open {
			transform: none;
		}
		.scrim {
			display: block;
			position: fixed;
			inset: 0;
			z-index: 55;
			background: var(--scrim);
		}
	}
</style>
