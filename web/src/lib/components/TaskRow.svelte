<script lang="ts">
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import { app } from '#lib/state.svelte.ts';
	import type { Task } from '#lib/api.ts';
	import Checkbox from './Checkbox.svelte';
	import Markdown from './Markdown.svelte';
	import TaskMeta from './TaskMeta.svelte';

	let { task, showProject = true }: { task: Task; showProject?: boolean } = $props();
	const done = $derived(task.status === 'done' || task.status === 'cancelled');
	let pressTimer: ReturnType<typeof setTimeout> | undefined;
	let pressStart = { x: 0, y: 0 };
	let suppressClick = false;

	function openMenuAt(x: number, y: number) {
		app.quick = { id: task.id, x, y };
	}
	function oncontextmenu(e: MouseEvent) {
		// Keep the native menu for links and Shift+right-click.
		if (e.shiftKey || (e.target as HTMLElement).closest('a')) return;
		e.preventDefault();
		openMenuAt(e.clientX, e.clientY);
	}
	function onpointerdown(e: PointerEvent) {
		if (e.pointerType !== 'touch') return;
		pressStart = { x: e.clientX, y: e.clientY };
		pressTimer = setTimeout(() => {
			suppressClick = true;
			navigator.vibrate?.(8);
			openMenuAt(pressStart.x, pressStart.y);
		}, 450);
	}
	function onpointermove(e: PointerEvent) {
		if (pressTimer && Math.hypot(e.clientX - pressStart.x, e.clientY - pressStart.y) > 8) cancelPress();
	}
	function cancelPress() {
		clearTimeout(pressTimer);
		pressTimer = undefined;
	}
	function onclick(e: MouseEvent) {
		if (suppressClick) {
			suppressClick = false;
			return;
		}
		if ((e.target as HTMLElement).closest('a')) return;
		app.drawer = task.id;
	}
	function ondragstart(e: DragEvent) {
		app.dragId = task.id;
		e.dataTransfer?.setData('text/plain', task.title);
		if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
	}
	function onkeydown(e: KeyboardEvent) {
		if (e.target !== e.currentTarget) return;
		const k = e.key;
		if (k === 'Enter') app.drawer = task.id;
		else if (k === 'x') app.toggleDone(task.id);
		else if (k === '.' || (k === 'F10' && e.shiftKey) || k === 'ContextMenu') {
			const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
			openMenuAt(r.left + 40, r.bottom);
		} else if (/^[0-4]$/.test(k)) app.update(task.id, { priority: Number(k) });
		else if (k === 'Delete' || k === 'Backspace') app.remove(task.id);
		else if (k === 'j' || k === 'ArrowDown') ((e.currentTarget as HTMLElement).nextElementSibling as HTMLElement | null)?.focus();
		else if (k === 'k' || k === 'ArrowUp') ((e.currentTarget as HTMLElement).previousElementSibling as HTMLElement | null)?.focus();
		else return;
		e.preventDefault();
	}
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<div
	class="row"
	class:done
	class:selected={app.drawer === task.id}
	role="listitem"
	tabindex="0"
	data-task-id={task.id}
	draggable="true"
	{ondragstart}
	ondragend={() => (app.dragId = null)}
	{oncontextmenu}
	{onpointerdown}
	{onpointermove}
	onpointerup={cancelPress}
	onpointercancel={cancelPress}
	{onclick}
	{onkeydown}
>
	<Checkbox {done} priority={task.priority} onclick={() => app.toggleDone(task.id)} />
	<span class="title"><Markdown src={task.title} inline /></span>
	<TaskMeta {task} {showProject} />
	<button
		class="more"
		aria-label="Quick actions"
		onclick={(e) => {
			e.stopPropagation();
			const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
			openMenuAt(r.left, r.bottom);
		}}><Ellipsis size={16} /></button
	>
</div>

<style>
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 44px;
		padding: var(--row-pad) 8px;
		border-bottom: 1px solid var(--border);
		border-radius: var(--r-sm);
		cursor: default;
		outline: none;
		-webkit-touch-callout: none;
		user-select: none;
		transition: background var(--dur) var(--ease);
	}
	.row:hover {
		background: var(--hover);
	}
	.row:focus-visible,
	.row.selected {
		background: var(--selected);
		box-shadow: inset 0 0 0 1.5px var(--accent);
	}
	.title {
		flex: 1 1 auto;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.done .title {
		color: var(--muted);
		text-decoration: line-through;
		text-decoration-color: var(--border-strong);
	}
	.more {
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		flex-shrink: 0;
		border: 0;
		border-radius: var(--r-sm);
		background: transparent;
		color: var(--muted);
		opacity: 0;
	}
	.row:hover .more,
	.row:focus-within .more,
	.more:focus-visible {
		opacity: 1;
	}
	.more:hover {
		background: var(--chip);
	}
	@media (hover: none) {
		.more {
			opacity: 1;
		}
	}
	@media (max-width: 640px) {
		.row {
			flex-wrap: wrap;
			row-gap: 2px;
		}
		.row :global(.meta) {
			order: 3;
			flex-basis: 100%;
			padding-left: 30px;
		}
	}
</style>
