<script lang="ts">
	let {
		done,
		priority = 0,
		size = 18,
		label = 'Complete',
		onclick
	}: { done: boolean; priority?: number; size?: number; label?: string; onclick: (e: MouseEvent) => void } = $props();
</script>

<button
	type="button"
	class="check p{priority}"
	class:done
	style="--s: {size}px"
	aria-label={done ? 'Mark as not done' : label}
	aria-pressed={done}
	onclick={(e) => {
		e.stopPropagation();
		onclick(e);
	}}
>
	{#if done}
		<svg viewBox="0 0 24 24" width={size - 6} height={size - 6} fill="none" stroke="currentColor" stroke-width="3.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5" /></svg>
	{/if}
</button>

<style>
	.check {
		width: var(--s);
		height: var(--s);
		flex-shrink: 0;
		display: grid;
		place-items: center;
		padding: 0;
		border-radius: var(--r-check);
		border: 1.6px solid var(--faint);
		background: transparent;
		color: var(--on-accent);
		transition: transform 120ms var(--ease), background var(--dur) var(--ease);
		position: relative;
	}
	/* Bigger hit target than the visual circle. */
	.check::after {
		content: '';
		position: absolute;
		inset: -10px;
	}
	.check:hover {
		transform: scale(1.08);
	}
	.p1 { border: 2px solid var(--p1); background: var(--p1-soft); }
	.p2 { border: 2px solid var(--p2); background: var(--p2-soft); }
	.p3 { border: 2px solid var(--p3); background: var(--p3-soft); }
	.p4 { border: 2px solid var(--p4); background: var(--p4-soft); }
	.done {
		background: var(--done);
		border-color: var(--done);
	}
</style>
