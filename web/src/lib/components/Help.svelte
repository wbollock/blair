<script lang="ts">
	let { open = $bindable(false) }: { open?: boolean } = $props();
	const keys = [
		['q / n', 'Add a task'],
		['Shift + Enter', 'Add and open details'],
		['/', 'Search'],
		['g then i · t · u · n', 'Go to Inbox · Today · Upcoming · Notes'],
		['t', 'Triage the inbox'],
		['b', 'List / board (in a project)'],
		['Right-click or .', 'Quick actions on a task'],
		['j / k', 'Move between tasks'],
		['x', 'Complete'],
		['0 – 4', 'Set priority'],
		['Enter', 'Open task'],
		['Delete', 'Move to trash'],
		['Esc', 'Close panel']
	];
</script>

{#if open}
	<div class="scrim" onclick={() => (open = false)} aria-hidden="true"></div>
	<div class="help" role="dialog" aria-modal="true" aria-label="Keyboard shortcuts">
		<h2>Keyboard shortcuts</h2>
		<dl>
			{#each keys as [k, v] (k)}
				<dt><kbd>{k}</kbd></dt>
				<dd>{v}</dd>
			{/each}
		</dl>
		<button class="btn" onclick={() => (open = false)}>Close</button>
	</div>
{/if}

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 90;
		background: var(--scrim);
	}
	.help {
		position: fixed;
		z-index: 91;
		top: 50%;
		left: 50%;
		transform: translate(-50%, -50%);
		width: min(460px, calc(100% - 32px));
		padding: 22px;
		border-radius: var(--r);
		background: var(--menu);
		box-shadow: var(--shadow-float);
	}
	h2 {
		margin: 0 0 14px;
		font-family: var(--font-display);
		font-weight: 600;
	}
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 8px 16px;
		margin: 0 0 16px;
	}
	dd {
		margin: 0;
		color: var(--muted);
	}
	kbd {
		font-family: var(--font-mono);
		font-size: 12px;
		padding: 2px 6px;
		border-radius: 4px;
		border: 1px solid var(--border-strong);
		background: var(--surface);
	}
</style>
