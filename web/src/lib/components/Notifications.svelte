<script lang="ts">
	import { Popover } from 'bits-ui';
	import Bell from '@lucide/svelte/icons/bell';
	import ShieldAlert from '@lucide/svelte/icons/shield-alert';
	import { app } from '#lib/state.svelte.ts';
	import { relativeTime } from '#lib/dates.ts';
	import type { Notification } from '#lib/api.ts';
	import Markdown from './Markdown.svelte';

	let open = $state(false);
	function select(n: Notification) {
		app.markRead([n.id]);
		if (n.task_id) {
			app.drawer = n.task_id;
			open = false;
		}
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger class="bell" aria-label={app.unread ? `Notifications, ${app.unread} unread` : 'Notifications'}>
		<Bell size={18} />
		{#if app.unread}<span class="badge">{app.unread > 9 ? '9+' : app.unread}</span>{/if}
	</Popover.Trigger>
	<Popover.Portal>
		<Popover.Content class="notif-panel" side="bottom" align="start" sideOffset={6} collisionPadding={8} aria-label="Notifications">
			<header>
				<span class="eyebrow">Notifications</span>
				{#if app.unread}<button class="all" onclick={() => app.markRead()}>Mark all read</button>{/if}
			</header>
			{#each app.notifications as n (n.id)}
				<button class="n" class:unread={!n.read_at} onclick={() => select(n)}>
					<span class="icon">{#if n.kind === 'reminder'}<Bell size={14} />{:else}<ShieldAlert size={14} />{/if}</span>
					<span class="body">
						{#if n.kind === 'reminder'}
							<span class="what">{#if n.task_title}<Markdown src={n.task_title} inline />{:else}<i>Deleted task</i>{/if}</span>
						{:else}
							<span class="what">{n.text}</span>
						{/if}
						<span class="when">{relativeTime(n.created_at)}</span>
					</span>
				</button>
			{:else}
				<p class="empty">Reminders and security alerts show up here.</p>
			{/each}
		</Popover.Content>
	</Popover.Portal>
</Popover.Root>

<style>
	:global(.bell) {
		position: relative;
		display: grid;
		place-items: center;
		width: 34px;
		height: 34px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
	}
	:global(.bell:hover) {
		background: var(--hover);
		color: var(--text);
	}
	.badge {
		position: absolute;
		top: 3px;
		right: 2px;
		min-width: 16px;
		height: 16px;
		padding: 0 4px;
		border-radius: 8px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: 10px;
		font-weight: 700;
		line-height: 16px;
		text-align: center;
	}
	:global(.notif-panel) {
		z-index: 60;
		width: min(360px, calc(100vw - 32px));
		max-height: min(480px, 70dvh);
		overflow-y: auto;
		padding: 6px;
		border: 1px solid var(--border);
		border-radius: var(--r);
		background: var(--menu);
		box-shadow: var(--shadow-float);
		outline: none;
	}
	header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: 6px 8px 8px;
	}
	.all {
		border: 0;
		background: none;
		padding: 0;
		font-size: 12px;
		color: var(--accent-ink);
		font-weight: 600;
	}
	.n {
		display: flex;
		gap: 10px;
		width: 100%;
		padding: 9px 8px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		text-align: left;
		color: var(--muted);
	}
	.n:hover {
		background: var(--hover);
	}
	.n.unread {
		color: var(--text);
	}
	.n.unread .icon {
		color: var(--accent);
	}
	.icon {
		padding-top: 2px;
		color: var(--faint);
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		font-size: 13px;
	}
	.when {
		font-size: 11px;
		color: var(--faint);
	}
	.empty {
		margin: 0;
		padding: 16px 8px;
		font-size: 13px;
		color: var(--muted);
	}
</style>
