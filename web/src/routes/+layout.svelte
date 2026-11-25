<script lang="ts">
	import '../app.css';
	import { onMount, type Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Menu from '@lucide/svelte/icons/menu';
	import { setUnauthorizedHandler } from '#lib/api.ts';
	import { app, applyTheme } from '#lib/state.svelte.ts';
	import Sidebar from '#lib/components/Sidebar.svelte';
	import TabBar from '#lib/components/TabBar.svelte';
	import TaskDrawer from '#lib/components/TaskDrawer.svelte';
	import QuickActions from '#lib/components/QuickActions.svelte';
	import CaptureSheet from '#lib/components/CaptureSheet.svelte';
	import Toasts from '#lib/components/Toasts.svelte';
	import Help from '#lib/components/Help.svelte';
	import Notifications from '#lib/components/Notifications.svelte';
	import Palette, { palette } from '#lib/components/Palette.svelte';
	import Search from '@lucide/svelte/icons/search';
	import { focusQuickAdd } from '#lib/components/QuickAdd.svelte';

	let { children }: { children: Snippet } = $props();
	const PUBLIC = ['/login', '/setup', '/register'];
	const isPublic = $derived(PUBLIC.includes(page.url.pathname));
	let ready = $state(false);
	let help = $state(false);
	let pendingG = false;

	onMount(() => {
		try {
			applyTheme(localStorage.getItem('blair.theme') ?? 'system');
		} catch {
			/* ignore */
		}
		setUnauthorizedHandler(() => {
			if (!PUBLIC.includes(location.pathname)) {
				app.reset();
				goto(`/login?next=${encodeURIComponent(location.pathname)}`);
			}
		});
		app.online = navigator.onLine;
		addEventListener('online', () => {
			app.online = true;
			app.refresh();
		});
		addEventListener('offline', () => (app.online = false));
		// Catch up after the app was in the background, and poll notifications while it's visible.
		let hiddenAt = 0;
		const onVisibility = () => {
			if (document.visibilityState === 'hidden') hiddenAt = Date.now();
			else if (hiddenAt && Date.now() - hiddenAt > 15_000) app.refresh();
		};
		document.addEventListener('visibilitychange', onVisibility);
		const poll = setInterval(() => document.visibilityState === 'visible' && app.pollNotifications(), 30_000);
		init();
		return () => {
			document.removeEventListener('visibilitychange', onVisibility);
			clearInterval(poll);
		};
	});

	async function init() {
		try {
			const st = await app.loadSession();
			const path = location.pathname;
			if (st.setup_required && path !== '/setup') await goto('/setup' + location.search);
			else if (!st.authenticated && !PUBLIC.includes(path)) await goto(`/login?next=${encodeURIComponent(path)}`);
			else if (st.authenticated && PUBLIC.includes(path)) await goto('/today');
			else if (st.authenticated && path === '/') await goto('/today');
			if (st.authenticated && new URLSearchParams(location.search).get('capture')) app.captureOpen = true;
			if (st.authenticated) app.pollNotifications();
		} catch {
			app.toast('Could not reach the server', 'error');
		} finally {
			ready = true;
		}
	}

	function typing(e: KeyboardEvent) {
		const el = e.target as HTMLElement;
		return el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || !!el.closest('[role="menu"]');
	}
	function onkeydown(e: KeyboardEvent) {
		if (!isPublic && app.me && (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			palette.open = !palette.open;
			return;
		}
		if (isPublic || !app.me || e.metaKey || e.ctrlKey || e.altKey || typing(e)) return;
		if (pendingG) {
			pendingG = false;
			const dest = { i: '/inbox', t: '/today', u: '/upcoming', n: '/notes', s: '/settings' }[e.key];
			if (dest) {
				e.preventDefault();
				goto(dest);
			}
			return;
		}
		switch (e.key) {
			case 'q':
			case 'n':
				e.preventDefault();
				if (matchMedia('(max-width: 640px)').matches) app.captureOpen = true;
				else focusQuickAdd();
				break;
			case '/':
				e.preventDefault();
				goto('/search');
				break;
			case 'g':
				pendingG = true;
				setTimeout(() => (pendingG = false), 1200);
				break;
			case 't':
				goto('/triage');
				break;
			case '?':
				help = true;
				break;
		}
	}
</script>

<svelte:window {onkeydown} />

{#if !ready}
	<div class="boot" aria-busy="true"></div>
{:else if isPublic || !app.me}
	{@render children()}
{:else}
	<div class="shell">
		<Sidebar />
		<main>
			<div class="top-actions">
				<button class="menu-btn" aria-label="Jump to…" onclick={() => (palette.open = true)}><Search size={19} /></button>
				<Notifications />
				<button class="menu-btn" aria-label="Open menu" onclick={() => (app.sidebarOpen = true)}><Menu size={20} /></button>
			</div>
			{#if !app.online}<div class="offline">You're offline. New tasks are kept on this device and sync when you reconnect; other changes won't save.</div>{/if}
			{@render children()}
		</main>
	</div>
	<TabBar />
	<TaskDrawer />
	<QuickActions />
	<CaptureSheet />
	<Help bind:open={help} />
	<Palette />
{/if}
<Toasts />

<style>
	.boot {
		min-height: 100dvh;
	}
	.shell {
		display: flex;
		min-height: 100dvh;
	}
	main {
		flex: 1;
		min-width: 0;
		padding: 30px clamp(16px, 4vw, 48px) 80px;
		position: relative;
	}
	.top-actions {
		display: none;
	}
	.offline {
		margin-bottom: 14px;
		padding: 8px 12px;
		border-radius: var(--r-sm);
		background: var(--p2-soft);
		font-size: 13px;
	}
	@media (max-width: 1023px) {
		.top-actions {
			display: flex;
			align-items: center;
			gap: 2px;
			position: absolute;
			top: calc(22px + env(safe-area-inset-top));
			right: 12px;
		}
		.menu-btn {
			display: grid;
			place-items: center;
			width: 40px;
			height: 40px;
			border: 0;
			border-radius: var(--r-sm);
			background: none;
			color: var(--muted);
		}
	}
	@media (max-width: 640px) {
		main {
			padding: calc(20px + env(safe-area-inset-top)) 16px calc(var(--tabbar-h) + 40px + env(safe-area-inset-bottom));
		}
	}
</style>
