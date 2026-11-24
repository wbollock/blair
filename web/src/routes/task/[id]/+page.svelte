<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { app } from '#lib/state.svelte.ts';

	// Deep link: open the task drawer over Today.
	onMount(async () => {
		const id = page.params.id as string;
		await app.refreshTask(id);
		await goto('/today', { replaceState: true });
		if (app.tasks[id]) app.drawer = id;
		else app.toast('That task no longer exists', 'error');
	});
</script>
