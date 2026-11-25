import { app, isNetworkError, type ViewInfo } from './state.svelte.ts';

/** Registers the current view (for the just-added tray) and loads its tasks. */
export function useView(info: () => ViewInfo, load: () => Promise<unknown>) {
	let loading = $state(true);
	$effect(() => {
		const v = info();
		app.view = v;
		return () => {
			if (app.view === v) app.view = null;
		};
	});
	let loaded = false;
	$effect(() => {
		void app.refreshSeq; // reload when the app comes back into view
		loading = !loaded; // refreshes update in place without the empty-state flash
		load()
			.then(() => (loaded = true))
			.catch((e) => !isNetworkError(e) && app.toast(e instanceof Error ? e.message : 'Could not load tasks', 'error'))
			.finally(() => (loading = false));
	});
	return {
		get loading() {
			return loading;
		}
	};
}
