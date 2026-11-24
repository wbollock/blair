import { goto } from '$app/navigation';
import { api, ApiError, ok, uuidv7, type AuthStatus, type Label, type Me, type Project, type Status, type Task } from './api.ts';
import { today } from './dates.ts';
import { parseQuickAdd, type QAResult } from './quickadd.ts';

/** The current view registers how it filters tasks, so the just-added tray can
 * tell when a new task would be invisible here (SPEC §5.3 "void" fix). */
export interface ViewInfo {
	name: string;
	href: string;
	matches: (t: Task) => boolean;
	projectId?: string;
	columnId?: string;
	dueDefault?: string;
	labelId?: string;
}

export interface Toast {
	id: number;
	message: string;
	tone: 'info' | 'error';
	actions: { label: string; run: () => void }[];
}

export type TaskPatch = Partial<Pick<Task, 'title' | 'description' | 'status' | 'priority' | 'project_id' | 'column_id' | 'due_date' | 'due_time' | 'labels'>>;

const OPEN: Status[] = ['todo', 'doing', 'waiting'];
export const isOpen = (t: Task) => OPEN.includes(t.status) && !t.deleted_at;

class AppState {
	status = $state<AuthStatus | null>(null);
	me = $state<Me | null>(null);
	projects = $state<Project[]>([]);
	labels = $state<Label[]>([]);
	tasks = $state<Record<string, Task>>({});
	justAdded = $state<string[]>([]);
	view = $state<ViewInfo | null>(null);
	toasts = $state<Toast[]>([]);
	drawer = $state<string | null>(null);
	quick = $state<{ id: string; x: number; y: number } | null>(null);
	captureOpen = $state(false);
	/** Task being dragged (rows, tray cards, board cards) so any drop target can accept it. */
	dragId = $state<string | null>(null);
	sidebarOpen = $state(false);
	online = $state(true);
	private toastSeq = 0;

	get tz(): string {
		return this.me?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	}
	get today(): string {
		return today(this.tz);
	}
	get theme(): string {
		const s = this.me?.settings as Record<string, unknown> | undefined;
		return (s?.theme as string) || 'system';
	}
	get inboxCount(): number {
		return Object.values(this.tasks).filter((t) => t.project_id === null && isOpen(t)).length;
	}

	// ---- session ----

	async loadSession(): Promise<AuthStatus> {
		this.status = await ok(api.GET('/auth/status'));
		if (this.status.authenticated) {
			this.me = await ok(api.GET('/me'));
			applyTheme(this.theme);
			await Promise.all([this.loadProjects(), this.loadLabels()]);
		}
		return this.status;
	}

	async logout() {
		// Leave the app first so in-flight 401s don't add a ?next= back to this page.
		await goto('/login');
		await api.POST('/auth/logout');
		this.reset();
	}

	reset() {
		this.me = null;
		this.status = null;
		this.projects = [];
		this.labels = [];
		this.tasks = {};
		this.justAdded = [];
		this.drawer = null;
	}

	async saveSettings(patch: Record<string, unknown>) {
		const settings = { ...((this.me?.settings as object) ?? {}), ...patch };
		this.me = await ok(api.PATCH('/me', { body: { settings } }));
		applyTheme(this.theme);
	}

	// ---- reference data ----

	async loadProjects() {
		this.projects = (await ok(api.GET('/projects'))).data;
	}
	async loadLabels() {
		this.labels = (await ok(api.GET('/labels'))).data;
	}
	project(id: string | null | undefined): Project | undefined {
		return id ? this.projects.find((p) => p.id === id) : undefined;
	}
	column(id: string | null | undefined) {
		if (!id) return undefined;
		for (const p of this.projects) {
			const c = p.columns.find((c) => c.id === id);
			if (c) return c;
		}
	}
	label(id: string) {
		return this.labels.find((l) => l.id === id);
	}

	// ---- tasks ----

	upsert(t: Task) {
		this.tasks[t.id] = t;
	}

	/** Loads every page of a task query into the store. */
	async loadTasks(query: Record<string, string>): Promise<Task[]> {
		const out: Task[] = [];
		let cursor: string | undefined;
		do {
			const page = await ok(api.GET('/tasks', { params: { query: { limit: 1000, ...query, ...(cursor ? { cursor } : {}) } as never } }));
			for (const t of page.data) this.upsert(t);
			out.push(...page.data);
			cursor = page.next_cursor ?? undefined;
		} while (cursor);
		return out;
	}

	async refreshTask(id: string) {
		try {
			this.upsert(await ok(api.GET('/tasks/{id}', { params: { path: { id } } })));
		} catch (e) {
			if (e instanceof ApiError && e.status === 404) delete this.tasks[id];
		}
	}

	parse(text: string, contextProjectId?: string): QAResult {
		const ctxProject = this.project(contextProjectId)?.name;
		return parseQuickAdd(text, {
			now: new Date(),
			tz: this.tz,
			weekStart: (this.me?.week_start as 'monday' | 'sunday') ?? 'monday',
			projects: this.projects.filter((p) => !p.archived_at).map((p) => ({ name: p.name, columns: p.columns.map((c) => c.name) })),
			labels: this.labels.map((l) => l.name),
			currentProject: ctxProject
		});
	}

	/** Quick-add: optimistic insert into the store and the just-added tray, then confirm. */
	async createFromText(text: string, opts: { open?: boolean } = {}): Promise<Task | null> {
		const v = this.view;
		const parsed = this.parse(text, v?.projectId);
		if (parsed.error) {
			this.toast('Add some text besides the tokens', 'error');
			return null;
		}
		const id = uuidv7();
		const body: Record<string, unknown> = { id, quick_add: text, create_missing: true };
		if (v?.projectId) body.context_project_id = v.projectId;
		if (!parsed.project && !parsed.projectNew && !parsed.column && v?.columnId) body.column_id = v.columnId;
		if (!parsed.dueDate && v?.dueDefault) body.due_date = v.dueDefault;
		if (v?.labelId && !parsed.labels.length) body.labels = [v.labelId];

		// Optimistic copy from the local parse.
		const proj = parsed.project ? this.projects.find((p) => p.name === parsed.project) : v?.projectId && !parsed.projectNew ? this.project(v.projectId) : undefined;
		const col = proj?.columns.find((c) => c.name === parsed.column) ?? proj?.columns.find((c) => c.id === body.column_id) ?? proj?.columns.find((c) => c.status === 'todo');
		const now = new Date().toISOString();
		this.upsert({
			id, title: parsed.title, description: '', status: col?.status ?? 'todo', priority: parsed.priority ?? 0,
			project_id: proj?.id ?? null, column_id: col?.id ?? null, due_date: parsed.dueDate ?? (body.due_date as string) ?? null,
			due_time: parsed.dueTime ?? null, labels: (body.labels as string[]) ?? this.labels.filter((l) => parsed.labels.includes(l.name)).map((l) => l.id),
			position: '~', completed_at: null, deleted_at: null, external_source: null, external_id: null, created_at: now, updated_at: now,
			version: 0, checklist_total: 0, checklist_done: 0, is_overdue: false
		});
		this.justAdded = [id, ...this.justAdded.filter((x) => x !== id)].slice(0, 8);
		if (opts.open) this.drawer = id;
		try {
			const t = await ok(api.POST('/tasks', { body: body as never }));
			this.upsert(t);
			if (parsed.projectNew) await this.loadProjects();
			if (parsed.labelsNew.length) await this.loadLabels();
			return t;
		} catch (e) {
			delete this.tasks[id];
			this.justAdded = this.justAdded.filter((x) => x !== id);
			if (this.drawer === id) this.drawer = null;
			this.toast(e instanceof Error ? e.message : 'Could not add the task', 'error');
			return null;
		}
	}

	async createTask(body: Record<string, unknown>): Promise<Task | null> {
		try {
			const t = await ok(api.POST('/tasks', { body: { id: uuidv7(), ...body } as never }));
			this.upsert(t);
			return t;
		} catch (e) {
			this.toast(e instanceof Error ? e.message : 'Could not add the task', 'error');
			return null;
		}
	}

	/** Optimistic PATCH with rollback. */
	async update(id: string, patch: TaskPatch, opts: { undo?: string } = {}): Promise<Task | null> {
		const prev = this.tasks[id];
		if (!prev) return null;
		const next: Task = { ...prev, ...patch };
		if (patch.column_id) {
			const c = this.column(patch.column_id);
			if (c) Object.assign(next, { status: c.status, project_id: c.project_id });
		} else if (patch.project_id !== undefined && patch.project_id !== prev.project_id) {
			const p = this.project(patch.project_id);
			const c = p?.columns.find((c) => c.status === next.status) ?? p?.columns.find((c) => c.status === 'todo');
			next.column_id = c?.id ?? null;
		} else if (patch.status && prev.project_id) {
			const c = this.project(prev.project_id)?.columns.find((c) => c.status === patch.status);
			if (c) next.column_id = c.id;
		}
		if (patch.due_date === null) next.due_time = null;
		const closed = next.status === 'done' || next.status === 'cancelled';
		next.completed_at = closed ? (prev.completed_at ?? new Date().toISOString()) : null;
		this.upsert(next);
		try {
			const t = await ok(api.PATCH('/tasks/{id}', { params: { path: { id } }, body: patch as never }));
			this.upsert(t);
			if (opts.undo) {
				const back: TaskPatch = {};
				for (const k of Object.keys(patch) as (keyof TaskPatch)[]) (back as Record<string, unknown>)[k] = prev[k];
				if (patch.column_id || patch.project_id !== undefined) Object.assign(back, { project_id: prev.project_id, column_id: prev.column_id });
				this.toast(opts.undo, 'info', [{ label: 'Undo', run: () => this.update(id, back) }]);
			}
			return t;
		} catch (e) {
			this.upsert(prev);
			this.toast(e instanceof Error ? e.message : 'Could not save', 'error');
			return null;
		}
	}

	async move(id: string, body: { project_id?: string | null; column_id?: string | null; before_id?: string; after_id?: string }) {
		const prev = this.tasks[id];
		if (!prev) return;
		const c = this.column(body.column_id ?? undefined);
		if (c) this.upsert({ ...prev, column_id: c.id, project_id: c.project_id, status: c.status });
		try {
			this.upsert(await ok(api.POST('/tasks/{id}/move', { params: { path: { id } }, body: body as never })));
		} catch (e) {
			this.upsert(prev);
			this.toast(e instanceof Error ? e.message : 'Could not move the task', 'error');
		}
	}

	async toggleDone(id: string) {
		const t = this.tasks[id];
		if (!t) return;
		const done = t.status === 'done' || t.status === 'cancelled';
		await this.update(id, { status: done ? 'todo' : 'done' }, { undo: done ? 'Reopened' : 'Completed' });
	}

	async remove(id: string) {
		const prev = this.tasks[id];
		if (!prev) return;
		delete this.tasks[id];
		this.justAdded = this.justAdded.filter((x) => x !== id);
		if (this.drawer === id) this.drawer = null;
		try {
			await ok(api.DELETE('/tasks/{id}', { params: { path: { id } } }));
			this.toast('Moved to trash', 'info', [
				{
					label: 'Undo',
					run: async () => {
						this.upsert(await ok(api.POST('/tasks/{id}/restore', { params: { path: { id } } })));
					}
				}
			]);
		} catch (e) {
			this.upsert(prev);
			this.toast(e instanceof Error ? e.message : 'Could not delete', 'error');
		}
	}

	async duplicate(id: string) {
		const t = this.tasks[id];
		if (!t) return;
		const copy = await this.createTask({
			title: t.title, description: t.description, priority: t.priority, project_id: t.project_id, column_id: t.column_id,
			due_date: t.due_date, due_time: t.due_time, labels: t.labels
		});
		if (copy) {
			this.justAdded = [copy.id, ...this.justAdded].slice(0, 8);
			this.toast('Duplicated');
		}
	}

	// ---- toasts ----

	toast(message: string, tone: Toast['tone'] = 'info', actions: Toast['actions'] = []) {
		const id = ++this.toastSeq;
		this.toasts = [...this.toasts.slice(-3), { id, message, tone, actions }];
		setTimeout(() => this.dismissToast(id), actions.length ? 8000 : 4000);
	}
	dismissToast(id: number) {
		this.toasts = this.toasts.filter((t) => t.id !== id);
	}
}

export const app = new AppState();

export function applyTheme(theme: string) {
	if (typeof document === 'undefined') return;
	const root = document.documentElement;
	if (theme === 'light' || theme === 'dark') root.dataset.theme = theme;
	else delete root.dataset.theme;
	try {
		localStorage.setItem('blair.theme', theme);
	} catch {
		/* storage unavailable */
	}
}

/** Sort helpers shared by list views. */
export const byPosition = (a: Task, b: Task) => (a.position < b.position ? -1 : a.position > b.position ? 1 : a.created_at.localeCompare(b.created_at));
export const byDue = (a: Task, b: Task) =>
	(a.due_date ?? '9999').localeCompare(b.due_date ?? '9999') || (a.due_time ?? '99').localeCompare(b.due_time ?? '99') || b.priority - a.priority || byPosition(a, b);
