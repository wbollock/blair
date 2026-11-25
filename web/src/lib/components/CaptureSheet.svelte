<script lang="ts">
	import { api, ok, uuidv7 } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import QuickAdd from './QuickAdd.svelte';
	import JustAddedTray from './JustAddedTray.svelte';

	let mode = $state<'task' | 'note'>('task');
	let note = $state('');
	let saving = $state(false);
	let startY = 0;

	async function saveNote() {
		if (!note.trim()) return;
		saving = true;
		try {
			await ok(api.POST('/notes', { body: { id: uuidv7(), body: note } }));
			note = '';
			app.toast('Note saved', 'info', [{ label: 'Open notes', run: () => (location.href = '/notes') }]);
		} catch (e) {
			app.toast(e instanceof Error ? e.message : 'Could not save the note', 'error');
		} finally {
			saving = false;
		}
	}
	const close = () => (app.captureOpen = false);
</script>

{#if app.captureOpen}
	<div class="scrim" onclick={close} aria-hidden="true"></div>
	<div
		class="sheet"
		role="dialog"
		aria-modal="true"
		aria-label="Quick capture"
		tabindex="-1"
		ontouchstart={(e) => (startY = e.touches[0].clientY)}
		ontouchend={(e) => e.changedTouches[0].clientY - startY > 90 && (e.target as HTMLElement).closest('.grab') && close()}
		onkeydown={(e) => e.key === 'Escape' && close()}
	>
		<button class="grab" aria-label="Close" onclick={close}><span></span></button>
		<div class="seg" role="tablist" aria-label="Capture type">
			<button role="tab" aria-selected={mode === 'task'} class:on={mode === 'task'} onclick={() => (mode = 'task')}>Task</button>
			<button role="tab" aria-selected={mode === 'note'} class:on={mode === 'note'} onclick={() => (mode = 'note')}>Note</button>
		</div>
		{#if mode === 'task'}
			<div class="tray"><JustAddedTray max={3} /></div>
			<QuickAdd autofocus compact />
			<p class="hint">#project · /column · @label · !1–4 · “fri 9am” · “every mon”</p>
		{:else}
			<!-- svelte-ignore a11y_autofocus -->
			<textarea class="input note" bind:value={note} autofocus placeholder="Write it down… markdown works"></textarea>
			<button class="btn btn-primary" disabled={!note.trim() || saving} onclick={saveNote}>Save note</button>
		{/if}
	</div>
{/if}

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 65;
		background: var(--scrim);
	}
	.sheet {
		position: fixed;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 70;
		max-height: 88dvh;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 6px 16px calc(16px + env(safe-area-inset-bottom));
		background: var(--bg);
		border-radius: 20px 20px 0 0;
		box-shadow: var(--shadow-float);
		animation: rise 220ms var(--ease);
		outline: none;
	}
	@keyframes rise {
		from {
			transform: translateY(40px);
			opacity: 0;
		}
	}
	.grab {
		align-self: center;
		padding: 10px 30px;
		border: 0;
		background: none;
	}
	.grab span {
		display: block;
		width: 40px;
		height: 5px;
		border-radius: 99px;
		background: var(--border-strong);
	}
	.seg {
		align-self: center;
		display: flex;
		padding: 3px;
		gap: 2px;
		border-radius: var(--r);
		background: var(--chip);
	}
	.seg button {
		min-width: 92px;
		min-height: 36px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
	}
	.seg .on {
		background: var(--surface);
		color: var(--text);
		font-weight: 600;
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.12);
	}
	.note {
		min-height: 180px;
		resize: vertical;
		font-size: 15px;
	}
	.hint {
		margin: 0;
		font-size: 12px;
		color: var(--muted);
		text-align: center;
	}
	@media (min-width: 641px) {
		.sheet {
			left: 50%;
			right: auto;
			bottom: auto;
			top: 12vh;
			width: 560px;
			transform: translateX(-50%);
			border-radius: var(--r);
			animation: none;
		}
	}
</style>
