<script lang="ts">
	import { page } from '$app/state';
	import Inbox from '@lucide/svelte/icons/inbox';
	import Sun from '@lucide/svelte/icons/sun';
	import Plus from '@lucide/svelte/icons/plus';
	import FolderKanban from '@lucide/svelte/icons/folder-kanban';
	import StickyNote from '@lucide/svelte/icons/sticky-note';
	import { app } from '#lib/state.svelte.ts';

	const path = $derived(page.url.pathname);
</script>

<nav class="tabbar" aria-label="Tabs">
	<a href="/inbox" class:active={path === '/inbox'}><span class="ic"><Inbox size={21} />{#if app.inboxCount}<span class="badge">{app.inboxCount}</span>{/if}</span>Inbox</a>
	<a href="/today" class:active={path === '/today'}><Sun size={21} />Today</a>
	<button class="add" aria-label="Quick capture" onclick={() => (app.captureOpen = true)}><Plus size={26} /></button>
	<button class:active={path.startsWith('/project')} onclick={() => (app.sidebarOpen = true)}><FolderKanban size={21} />Projects</button>
	<a href="/notes" class:active={path === '/notes'}><StickyNote size={21} />Notes</a>
</nav>

<style>
	.tabbar {
		display: none;
	}
	@media (max-width: 640px) {
		.tabbar {
			position: fixed;
			left: 0;
			right: 0;
			bottom: 0;
			z-index: 35;
			display: grid;
			grid-template-columns: repeat(5, 1fr);
			align-items: center;
			height: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
			padding-bottom: env(safe-area-inset-bottom);
			background: color-mix(in srgb, var(--bg) 92%, transparent);
			backdrop-filter: blur(12px);
			border-top: 1px solid var(--border);
		}
		a,
		button {
			display: flex;
			flex-direction: column;
			align-items: center;
			gap: 2px;
			min-height: 48px;
			justify-content: center;
			border: 0;
			background: none;
			color: var(--muted);
			font-size: 11px;
			text-decoration: none;
		}
		.active {
			color: var(--accent-ink);
			font-weight: 600;
		}
		.ic {
			position: relative;
			display: grid;
		}
		.badge {
			position: absolute;
			top: -4px;
			right: -10px;
			min-width: 16px;
			height: 16px;
			padding: 0 4px;
			border-radius: 99px;
			background: var(--accent);
			color: var(--on-accent);
			font-size: 10px;
			font-weight: 700;
			display: grid;
			place-items: center;
		}
		.add {
			justify-self: center;
			width: 52px;
			height: 52px;
			min-height: 0;
			border-radius: 16px;
			background: var(--accent);
			color: var(--on-accent);
			box-shadow: var(--shadow-card);
		}
	}
</style>
