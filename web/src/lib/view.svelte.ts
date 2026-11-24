import { app, type ViewInfo } from './state.svelte.ts';

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
	$effect(() => {
		loading = true;
		load()
			.catch((e) => app.toast(e instanceof Error ? e.message : 'Could not load tasks', 'error'))
			.finally(() => (loading = false));
	});
	return {
		get loading() {
			return loading;
		}
	};
}
