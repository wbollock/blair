<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import { app } from '#lib/state.svelte.ts';
	import StatusItems from './menus/StatusItems.svelte';
	import ColumnItems from './menus/ColumnItems.svelte';
	import ProjectItems from './menus/ProjectItems.svelte';
	import DueItems from './menus/DueItems.svelte';
	import PriorityRow from './menus/PriorityRow.svelte';
	import LabelItems from './menus/LabelItems.svelte';

	// One shared menu for every task (SPEC §4.4), anchored at the pointer or the row.
	const task = $derived(app.quick ? app.tasks[app.quick.id] : undefined);
	let open = $state(false);
	$effect(() => {
		open = !!task;
	});
	const anchor = $derived.by(() => {
		const q = app.quick;
		if (!q) return null;
		return { getBoundingClientRect: () => new DOMRect(q.x, q.y, 0, 0) };
	});
	const closed = $derived(task?.status === 'done' || task?.status === 'cancelled');

	async function copyLink() {
		if (!task) return;
		await navigator.clipboard?.writeText(`${location.origin}/task/${task.id}`);
		app.toast('Link copied');
	}
	async function copyMarkdown() {
		if (!task) return;
		await navigator.clipboard?.writeText(`- [${closed ? 'x' : ' '}] ${task.title}`);
		app.toast('Copied as markdown');
	}
</script>

{#if task}
	<DropdownMenu.Root bind:open onOpenChange={(o) => !o && (app.quick = null)}>
		<DropdownMenu.Portal>
			<DropdownMenu.Content class="menu" customAnchor={anchor} side="bottom" align="start" sideOffset={4} collisionPadding={8} aria-label="Quick actions">
				<DropdownMenu.Item class="menu-item" onSelect={() => (app.drawer = task.id)}>Open<span class="kbd">↵</span></DropdownMenu.Item>
				<DropdownMenu.Item class="menu-item" onSelect={() => app.toggleDone(task.id)}>{closed ? 'Reopen' : 'Complete'}<span class="kbd">X</span></DropdownMenu.Item>
				<DropdownMenu.Separator class="menu-sep" />
				<DropdownMenu.Sub>
					<DropdownMenu.SubTrigger class="menu-item">Status<span class="kbd">›</span></DropdownMenu.SubTrigger>
					<DropdownMenu.SubContent class="menu" sideOffset={6}><StatusItems {task} /></DropdownMenu.SubContent>
				</DropdownMenu.Sub>
				{#if task.project_id}
					<DropdownMenu.Sub>
						<DropdownMenu.SubTrigger class="menu-item">Column<span class="kbd">›</span></DropdownMenu.SubTrigger>
						<DropdownMenu.SubContent class="menu" sideOffset={6}><ColumnItems {task} /></DropdownMenu.SubContent>
					</DropdownMenu.Sub>
				{/if}
				<DropdownMenu.Sub>
					<DropdownMenu.SubTrigger class="menu-item">Move to project<span class="kbd">›</span></DropdownMenu.SubTrigger>
					<DropdownMenu.SubContent class="menu" sideOffset={6}><ProjectItems {task} /></DropdownMenu.SubContent>
				</DropdownMenu.Sub>
				<DropdownMenu.Sub>
					<DropdownMenu.SubTrigger class="menu-item">Due date<span class="kbd">›</span></DropdownMenu.SubTrigger>
					<DropdownMenu.SubContent class="menu" sideOffset={6}><DueItems {task} /></DropdownMenu.SubContent>
				</DropdownMenu.Sub>
				<DropdownMenu.Separator class="menu-sep" />
				<PriorityRow {task} />
				<DropdownMenu.Sub>
					<DropdownMenu.SubTrigger class="menu-item">Labels<span class="kbd">›</span></DropdownMenu.SubTrigger>
					<DropdownMenu.SubContent class="menu" sideOffset={6}><LabelItems {task} /></DropdownMenu.SubContent>
				</DropdownMenu.Sub>
				<DropdownMenu.Separator class="menu-sep" />
				<DropdownMenu.Item class="menu-item" onSelect={() => app.duplicate(task.id)}>Duplicate</DropdownMenu.Item>
				<DropdownMenu.Item class="menu-item" onSelect={copyLink}>Copy link</DropdownMenu.Item>
				<DropdownMenu.Item class="menu-item" onSelect={copyMarkdown}>Copy as markdown</DropdownMenu.Item>
				<DropdownMenu.Separator class="menu-sep" />
				<DropdownMenu.Item class="menu-item danger" onSelect={() => app.remove(task.id)}>Delete<span class="kbd">Del</span></DropdownMenu.Item>
			</DropdownMenu.Content>
		</DropdownMenu.Portal>
	</DropdownMenu.Root>
{/if}

<style>
	:global(.menu-item.danger) {
		color: var(--danger);
	}
</style>
