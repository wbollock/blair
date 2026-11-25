import { goto } from '$app/navigation';
import { api, ApiError, ok, uuidv7, type AuthStatus, type Label, type Me, type Notification, type Project, type Status, type Task } from './api.ts';
import { dueLabel, today } from './dates.ts';
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

export type TaskPatch = Partial<Pick<Task, 'title' | 'description' | 'status' | 'priority' | 'project_id' | 'column_id' | 'due_date' | 'due_time' | 'remind_at' | 'recurrence' | 'labels'>>;

/** A task created while offline, waiting to be sent (SPEC §12.3). */
export interface Pending {
	owner: string;
	task: Task;
	body: Record<string, unknown>;
	patch: TaskPatch;
	error?: string;
}

const OUTBOX = 'blair.outbox';
const BOOT = 'blair.boot';
function readStore<T>(key: string, fallback: T): T {
	try {
		return JSON.parse(localStorage.getItem(key) ?? '') as T;
	} catch {
		return fallback;
	}
}
function writeStore(key: string, value: unknown) {
	try {
		if (value === null) localStorage.removeItem(key);
		else localStorage.setItem(key, JSON.stringify(value));
	} catch {
		/* storage unavailable */
	}
}

/** fetch failed outright (offline, DNS, server down) rather than returning an error response. */
export const isNetworkError = (e: unknown) => !(e instanceof ApiError) && e instanceof Error;
const errMsg = (e: unknown, fallback: string) =>
	isNetworkError(e) ? "You're offline, so this change wasn't saved" : e instanceof Error ? e.message : fallback;

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
	notifications = $state<Notification[]>([]);
	unread = $state(0);
	/** Bumped when the app comes back into view; views reload their tasks. */
	refreshSeq = $state(0);
	outbox = $state<Pending[]>([]);
	/** Today's plan (task ids, in order) when the daily plan is turned on. */
	plan = $state<string[]>([]);
	private toastSeq = 0;
	private seen = new Set<string>();
	private notificationsPrimed = false;
	private replaying = false;

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
	/** The daily plan is optional (Settings → Features). */
	get planEnabled(): boolean {
		return (this.me?.settings as Record<string, unknown> | undefined)?.daily_plan === true;
	}
	get inboxCount(): number {
		return Object.values(this.tasks).filter((t) => t.project_id === null && isOpen(t)).length;
	}

	// ---- session ----

	/** Loads the session. Offline, it boots from the last good copy so capture still works. */
	async loadSession(): Promise<AuthStatus> {
		try {
			this.status = await ok(api.GET('/auth/status'));
			if (this.status.authenticated) {
				this.me = await ok(api.GET('/me'));
				await Promise.all([this.loadProjects(), this.loadLabels()]);
				writeStore(BOOT, { status: this.status, me: this.me, projects: this.projects, labels: this.labels });
			} else writeStore(BOOT, null);
		} catch (e) {
			const boot = readStore<{ status: AuthStatus; me: Me; projects: Project[]; labels: Label[] } | null>(BOOT, null);
			if (!isNetworkError(e) || !boot) throw e;
			Object.assign(this, boot);
			this.online = false;
		}
		if (this.me) {
			applyTheme(this.theme);
			this.restoreOutbox();
			void this.loadPlan();
		}
		return this.status!;
	}

	async logout() {
		const n = this.outbox.length;
		if (n && !confirm(`${n} task${n === 1 ? " hasn't" : "s haven't"} synced yet and will be lost. Sign out anyway?`)) return;
		// Leave the app first so in-flight 401s don't add a ?next= back to this page.
		await goto('/login');
		await api.POST('/auth/logout');
		this.outbox = [];
		writeStore(OUTBOX, null);
		this.reset();
	}

	/** Clears signed-in state. The outbox survives (an expired session shouldn't lose captures); it is tied to its owner. */
	reset() {
		this.me = null;
		this.status = null;
		this.projects = [];
		this.labels = [];
		this.tasks = {};
		this.justAdded = [];
		this.drawer = null;
		this.notifications = [];
		this.unread = 0;
		this.plan = [];
		this.seen.clear();
		this.notificationsPrimed = false;
		writeStore(BOOT, null);
	}

	/** Re-syncs after the app was in the background: reference data, notifications, outbox, and the current view. */
	async refresh() {
		if (!this.me) return;
		this.refreshSeq++;
		await Promise.allSettled([this.loadProjects(), this.loadLabels(), this.pollNotifications(), this.replayOutbox(), this.loadPlan()]);
		}

		// ---- daily plan (ADR-0014) ----

		async loadPlan() {
		if (!this.me || !this.planEnabled) return;
		try {
			const p = await ok(api.GET('/plans/{date}', { params: { path: { date: 'today' } } }));
			for (const t of p.tasks) this.upsert(t);
			this.plan = p.task_ids;
		} catch {
			/* offline; keep what we had */
		}
		}
		inPlan(id: string): boolean {
		return this.plan.includes(id);
		}
		async togglePlan(id: string, opts: { quiet?: boolean } = {}) {
		const prev = this.plan;
		const adding = !prev.includes(id);
		this.plan = adding ? [...prev, id] : prev.filter((x) => x !== id);
		try {
			if (adding) this.plan = (await ok(api.POST('/plans/{date}/tasks', { params: { path: { date: 'today' } }, body: { task_ids: [id] } }))).task_ids;
			else await ok(api.DELETE('/plans/{date}/tasks/{id}', { params: { path: { date: 'today', id } } }));
			if (!opts.quiet) this.toast(adding ? "Added to today's plan" : "Removed from today's plan", 'info', [{ label: 'Undo', run: () => this.togglePlan(id, { quiet: true }) }]);
		} catch (e) {
			this.plan = prev;
			this.toast(errMsg(e, 'Could not update the plan'), 'error');
		}
		}
		async addToPlan(ids: string[]) {
		if (!ids.length) return;
		try {
			const p = await ok(api.POST('/plans/{date}/tasks', { params: { path: { date: 'today' } }, body: { task_ids: ids } }));
			for (const t of p.tasks) this.upsert(t);
			this.plan = p.task_ids;
		} catch (e) {
			this.toast(errMsg(e, 'Could not update the plan'), 'error');
		}
		}

	// ---- notifications ----

	async pollNotifications() {
		if (!this.me) return;
		try {
			const page = await ok(api.GET('/notifications', { params: { query: { limit: 30 } } }));
			for (const n of page.data) {
				if (this.notificationsPrimed && !this.seen.has(n.id) && !n.read_at && n.kind === 'reminder' && n.task_id) {
					const id = n.task_id;
					this.toast(`Reminder: ${(n.task_title ?? 'a task').replace(/[*_`~]/g, '')}`, 'info', [{ label: 'Open', run: () => (this.drawer = id) }]);
				}
				this.seen.add(n.id);
			}
			this.notificationsPrimed = true;
			this.notifications = page.data;
			this.unread = page.unread_count;
		} catch {
			/* offline or signed out; the next poll catches up */
		}
	}

	async markRead(ids?: string[]) {
		const now = new Date().toISOString();
		const hit = (n: Notification) => !n.read_at && (!ids || ids.includes(n.id));
		this.unread = Math.max(0, this.unread - (ids ? this.notifications.filter(hit).length : this.unread));
		this.notifications = this.notifications.map((n) => (hit(n) ? { ...n, read_at: now } : n));
		try {
			await ok(api.POST('/me/notifications/read', { body: ids ? { ids } : {} }));
		} catch {
			/* harmless: they stay unread on the server until the next try */
		}
	}

	// ---- offline outbox ----

	pending(id: string): Pending | undefined {
		return this.outbox.find((p) => p.task.id === id);
	}
	private saveOutbox() {
		writeStore(OUTBOX, this.outbox.length ? this.outbox : null);
	}
	private restoreOutbox() {
		const mine = readStore<Pending[]>(OUTBOX, []).filter((p) => p.owner === this.me?.id);
		this.outbox = mine;
		this.saveOutbox();
		for (const p of mine) this.upsert(p.task);
		this.justAdded = [...new Set([...mine.map((p) => p.task.id), ...this.justAdded])].slice(0, 8);
		void this.replayOutbox();
	}
	discardPending(id: string) {
		this.outbox = this.outbox.filter((p) => p.task.id !== id);
		this.saveOutbox();
		delete this.tasks[id];
		this.justAdded = this.justAdded.filter((x) => x !== id);
	}

	/** Sends queued tasks in order. Creates are idempotent on the client id, so a retry never duplicates. */
	async replayOutbox() {
		if (this.replaying || !this.outbox.length || !this.me) return;
		this.replaying = true;
		let synced = 0;
		try {
			for (const p of [...this.outbox]) {
				try {
					let t = await ok(api.POST('/tasks', { body: p.body as never }));
					if (Object.keys(p.patch).length) t = await ok(api.PATCH('/tasks/{id}', { params: { path: { id: t.id } }, body: p.patch as never }));
					this.upsert(t);
					this.outbox = this.outbox.filter((x) => x !== p);
					synced++;
				} catch (e) {
					if (isNetworkError(e)) break; // still offline; keep the rest queued
					this.outbox = this.outbox.map((x) => (x === p ? { ...x, error: e instanceof Error ? e.message : 'Rejected by the server' } : x));
				}
				this.saveOutbox();
			}
		} finally {
			this.replaying = false;
		}
		if (synced) {
			this.online = true;
			this.toast(`Synced ${synced} task${synced === 1 ? '' : 's'} added offline`);
			await Promise.allSettled([this.loadProjects(), this.loadLabels()]);
		}
	}

	async saveSettings(patch: Record<string, unknown>) {
		const settings = { ...((this.me?.settings as object) ?? {}), ...patch };
		this.me = await ok(api.PATCH('/me', { body: { settings } }));
		applyTheme(this.theme);
		if ('daily_plan' in patch) await this.loadPlan();
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
			version: 0, checklist_total: 0, checklist_done: 0, is_overdue: false, remind_at: null, recurrence: parsed.recurrence ?? null
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
			if (isNetworkError(e) && this.me) {
				this.outbox = [...this.outbox, { owner: this.me.id, task: $state.snapshot(this.tasks[id]), body, patch: {} }];
				this.saveOutbox();
				this.online = false;
				this.toast("Saved on this device. It'll sync when you're back online.");
				return this.tasks[id];
			}
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
			this.toast(errMsg(e, 'Could not add the task'), 'error');
			return null;
		}
	}

	/** Optimistic PATCH with rollback. Edits to a queued offline task change its queued patch instead. */
	async update(id: string, patch: TaskPatch, opts: { undo?: string } = {}): Promise<Task | null> {
		const prev = this.tasks[id];
		if (!prev) return null;
		const queued = this.pending(id);
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
		if (queued) {
			this.outbox = this.outbox.map((p) => (p === queued ? { ...p, task: $state.snapshot(next), patch: { ...p.patch, ...patch }, error: undefined } : p));
			this.saveOutbox();
			return next;
		}
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
			this.toast(errMsg(e, 'Could not save'), 'error');
			return null;
		}
	}

	async move(id: string, body: { project_id?: string | null; column_id?: string | null; before_id?: string; after_id?: string }) {
		const prev = this.tasks[id];
		if (!prev) return;
		if (this.pending(id)) {
			await this.update(id, body.column_id ? { column_id: body.column_id } : { project_id: body.project_id ?? null });
			return;
		}
		const c = this.column(body.column_id ?? undefined);
		if (c) this.upsert({ ...prev, column_id: c.id, project_id: c.project_id, status: c.status });
		try {
			this.upsert(await ok(api.POST('/tasks/{id}/move', { params: { path: { id } }, body: body as never })));
		} catch (e) {
			this.upsert(prev);
			this.toast(errMsg(e, 'Could not move the task'), 'error');
		}
	}

	async toggleDone(id: string) {
		const t = this.tasks[id];
		if (!t) return;
		const done = t.status === 'done' || t.status === 'cancelled';
		if (!done && t.recurrence && !this.pending(id)) {
			// The server logs the completion and moves the task to its next date (ADR-0013).
			const prev = { due_date: t.due_date, due_time: t.due_time, remind_at: t.remind_at };
			const next = await this.update(id, { status: 'done' });
			if (next?.due_date && next.due_date !== prev.due_date) {
				this.toast(`Done · next ${dueLabel(next.due_date, next.due_time, this.tz)}`, 'info', [{ label: 'Undo', run: () => this.update(id, prev) }]);
			}
			return;
		}
		await this.update(id, { status: done ? 'todo' : 'done' }, { undo: done ? 'Reopened' : 'Completed' });
	}

	async remove(id: string) {
		const prev = this.tasks[id];
		if (!prev) return;
		if (this.pending(id)) return this.discardPending(id);
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
			this.toast(errMsg(e, 'Could not delete'), 'error');
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
