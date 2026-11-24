<script lang="ts">
	import { app } from '#lib/state.svelte.ts';
</script>

<div class="toasts" role="status" aria-live="polite">
	{#each app.toasts as t (t.id)}
		<div class="toast" class:error={t.tone === 'error'}>
			<span>{t.message}</span>
			{#each t.actions as a (a.label)}
				<button
					onclick={() => {
						a.run();
						app.dismissToast(t.id);
					}}>{a.label}</button
				>
			{/each}
		</div>
	{/each}
</div>

<style>
	.toasts {
		position: fixed;
		left: 50%;
		bottom: calc(24px + env(safe-area-inset-bottom));
		transform: translateX(-50%);
		display: flex;
		flex-direction: column;
		gap: 8px;
		z-index: 80;
		pointer-events: none;
	}
	.toast {
		display: flex;
		align-items: center;
		gap: 14px;
		min-height: 44px;
		padding: 8px 10px 8px 16px;
		border-radius: var(--r);
		background: var(--text);
		color: var(--bg);
		box-shadow: var(--shadow-float);
		font-size: 13.5px;
		pointer-events: auto;
		animation: up 200ms var(--ease);
	}
	.toast.error {
		background: var(--danger);
		color: #fff;
	}
	.toast button {
		border: 0;
		background: none;
		color: inherit;
		font-weight: 700;
		padding: 6px 8px;
		border-radius: var(--r-sm);
	}
	.toast button:hover {
		background: rgba(127, 127, 127, 0.25);
	}
	@keyframes up {
		from {
			opacity: 0;
			transform: translateY(8px);
		}
	}
	/* On phones the keyboard and capture sheet own the bottom; toasts go on top. */
	@media (max-width: 640px) {
		.toasts {
			top: calc(12px + env(safe-area-inset-top));
			bottom: auto;
			width: calc(100% - 32px);
		}
	}
</style>
