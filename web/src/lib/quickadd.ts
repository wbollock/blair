// Quick-add parser (SPEC §5.1). Mirrors internal/quickadd/quickadd.go; both must
// pass testdata/quickadd/cases.json. Token offsets here are UTF-16 indices.

export interface QAProject {
	name: string;
	columns: string[];
}

export interface QAContext {
	now: Date;
	tz: string;
	weekStart: 'monday' | 'sunday';
	projects: QAProject[];
	labels: string[];
	currentProject?: string;
}

export type TokenKind = 'project' | 'column' | 'label' | 'priority' | 'date';

export interface QAToken {
	kind: TokenKind;
	text: string;
	start: number;
	end: number;
	value: string;
}

export interface QAResult {
	title: string;
	project?: string;
	projectNew?: string;
	column?: string;
	/** /column for a projectNew, resolved after the project is created. */
	columnNew?: string;
	priority?: number;
	dueDate?: string;
	dueTime?: string;
	labels: string[];
	labelsNew: string[];
	tokens: QAToken[];
	error?: 'empty_title';
}

interface Word {
	text: string;
	start: number;
	end: number;
	escaped: boolean;
}

function split(input: string): Word[] {
	const out: Word[] = [];
	let i = 0;
	const ws = (c: string) => c === ' ' || c === '\t' || c === '\n';
	while (i < input.length) {
		while (i < input.length && ws(input[i])) i++;
		if (i >= input.length) break;
		const start = i;
		if (input.startsWith('#"', i)) {
			const j = input.indexOf('"', i + 2);
			if (j >= 0) {
				out.push({ text: input.slice(start, j + 1), start, end: j + 1, escaped: false });
				i = j + 1;
				continue;
			}
		}
		while (i < input.length && !ws(input[i])) i++;
		const w: Word = { text: input.slice(start, i), start, end: i, escaped: false };
		if (w.text.startsWith('\\') && w.text.length > 1) {
			w.text = w.text.slice(1);
			w.escaped = true;
		}
		out.push(w);
	}
	return out;
}

const reName = /^[\p{L}\p{N}_.-]+$/u;
const reInUnit = /^(\d+)(d|w|m)$/;
const reISO = /^(\d{4})-(\d{2})-(\d{2})$/;
const reSlash = /^(\d{1,2})\/(\d{1,2})(?:\/(\d{4}))?$/;
const reDayNum = /^(\d{1,2})(?:st|nd|rd|th)?$/;
const reClock24 = /^([01]?\d|2[0-3]):([0-5]\d)$/;
const reClock12 = /^(1[0-2]|0?[1-9])(?::([0-5]\d))?(am|pm)$/;
const reBareHour = /^(1[0-2]|0?[1-9])(?::([0-5]\d))?$/;

const priorities: Record<string, number> = {
	'1': 1, '2': 2, '3': 3, '4': 4, low: 1, med: 2, medium: 2, high: 3, urgent: 4
};

const weekdays: Record<string, number> = {
	sun: 0, sunday: 0, mon: 1, monday: 1, tue: 2, tues: 2, tuesday: 2,
	wed: 3, weds: 3, wednesday: 3, thu: 4, thur: 4, thurs: 4, thursday: 4,
	fri: 5, friday: 5, sat: 6, saturday: 6
};

const months: Record<string, number> = {
	jan: 1, january: 1, feb: 2, february: 2, mar: 3, march: 3, apr: 4, april: 4, may: 5,
	jun: 6, june: 6, jul: 7, july: 7, aug: 8, august: 8, sep: 9, sept: 9, september: 9,
	oct: 10, october: 10, nov: 11, november: 11, dec: 12, december: 12
};

/** A calendar date with no time zone. */
class CalDate {
	constructor(
		readonly y: number,
		readonly m: number,
		readonly d: number
	) {}
	static from(y: number, m: number, d: number): CalDate | null {
		if (m < 1 || m > 12 || d < 1 || d > 31) return null;
		const t = new Date(Date.UTC(y, m - 1, d));
		if (t.getUTCDate() !== d) return null;
		return new CalDate(y, m, d);
	}
	add(days: number): CalDate {
		const t = new Date(Date.UTC(this.y, this.m - 1, this.d + days));
		return new CalDate(t.getUTCFullYear(), t.getUTCMonth() + 1, t.getUTCDate());
	}
	addMonths(n: number): CalDate {
		const t = new Date(Date.UTC(this.y, this.m - 1 + n, this.d));
		return new CalDate(t.getUTCFullYear(), t.getUTCMonth() + 1, t.getUTCDate());
	}
	weekday(): number {
		return new Date(Date.UTC(this.y, this.m - 1, this.d)).getUTCDay();
	}
	cmp(o: CalDate): number {
		return this.y - o.y || this.m - o.m || this.d - o.d;
	}
	toString(): string {
		return `${this.y}-${String(this.m).padStart(2, '0')}-${String(this.d).padStart(2, '0')}`;
	}
}

/** Today's date in a time zone. */
export function todayIn(now: Date, tz: string): CalDate {
	const parts = new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now);
	const get = (t: string) => Number(parts.find((p) => p.type === t)?.value);
	return new CalDate(get('year'), get('month'), get('day'));
}

function normalize(s: string): string {
	return s.replace(/[\s_-]/gu, '').toLowerCase();
}

function matchName(names: string[], q: string): string | undefined {
	const lq = q.toLowerCase();
	const exact = names.find((n) => n.toLowerCase() === lq);
	if (exact) return exact;
	const nq = normalize(q);
	const norm = names.find((n) => normalize(n) === nq);
	if (norm) return norm;
	const pre = names.filter((n) => normalize(n).startsWith(nq));
	return pre.length === 1 ? pre[0] : undefined;
}

function rollForward(today: CalDate, m: number, d: number): CalDate | null {
	let t = CalDate.from(today.y, m, d);
	if (!t) return null;
	if (t.cmp(today) < 0) t = CalDate.from(today.y + 1, m, d);
	return t;
}

function matchDate(ws: string[], today: CalDate, weekStart: number): { date: string; time: string; n: number } | null {
	if (!ws.length) return null;
	const w0 = ws[0];
	if (w0 === 'today') return { date: today.toString(), time: '', n: 1 };
	if (w0 === 'tonight') return { date: today.toString(), time: '20:00', n: 1 };
	if (w0 === 'tomorrow' || w0 === 'tmr' || w0 === 'tmrw') return { date: today.add(1).toString(), time: '', n: 1 };
	if (w0 === 'yesterday') return { date: today.add(-1).toString(), time: '', n: 1 };
	if (w0 in weekdays) {
		let diff = (weekdays[w0] - today.weekday() + 7) % 7;
		if (diff === 0) diff = 7;
		return { date: today.add(diff).toString(), time: '', n: 1 };
	}
	if (w0 === 'next' && ws.length > 1) {
		if (ws[1] in weekdays) {
			const sinceStart = (today.weekday() - weekStart + 7) % 7;
			const nextWeek = today.add(7 - sinceStart);
			const off = (weekdays[ws[1]] - weekStart + 7) % 7;
			return { date: nextWeek.add(off).toString(), time: '', n: 2 };
		}
		if (ws[1] === 'week') return { date: today.add(7).toString(), time: '', n: 2 };
	}
	if (w0 === 'in' && ws.length > 1) {
		const m = reInUnit.exec(ws[1]);
		if (m) return { date: addUnit(today, Number(m[1]), m[2]), time: '', n: 2 };
		const k = Number(ws[1]);
		if (/^\d+$/.test(ws[1]) && ws.length > 2) {
			const unit = { day: 'd', days: 'd', week: 'w', weeks: 'w', month: 'm', months: 'm' }[ws[2]];
			if (unit) return { date: addUnit(today, k, unit), time: '', n: 3 };
		}
	}
	let m = reISO.exec(w0);
	if (m) {
		const t = CalDate.from(Number(m[1]), Number(m[2]), Number(m[3]));
		if (t) return { date: t.toString(), time: '', n: 1 };
	}
	m = reSlash.exec(w0);
	if (m) {
		const t = m[3] ? CalDate.from(Number(m[3]), Number(m[1]), Number(m[2])) : rollForward(today, Number(m[1]), Number(m[2]));
		if (t) return { date: t.toString(), time: '', n: 1 };
	}
	if (w0 in months && ws.length > 1) {
		const dm = reDayNum.exec(ws[1]);
		if (dm) {
			const t = rollForward(today, months[w0], Number(dm[1]));
			if (t) return { date: t.toString(), time: '', n: 2 };
		}
	}
	const dm = reDayNum.exec(w0);
	if (dm && ws.length > 1 && ws[1] in months) {
		const t = rollForward(today, months[ws[1]], Number(dm[1]));
		if (t) return { date: t.toString(), time: '', n: 2 };
	}
	return null;
}

function addUnit(t: CalDate, k: number, unit: string): string {
	if (unit === 'w') return t.add(7 * k).toString();
	if (unit === 'm') return t.addMonths(k).toString();
	return t.add(k).toString();
}

const pad = (n: number) => String(n).padStart(2, '0');

function clock12(hs: string, ms: string | undefined, ampm: string): string {
	let h = Number(hs);
	if (ampm === 'pm' && h !== 12) h += 12;
	if (ampm === 'am' && h === 12) h = 0;
	return `${pad(h)}:${ms ?? '00'}`;
}

function matchTime(ws: string[]): { time: string; n: number } | null {
	const off = ws[0] === 'at' ? 1 : 0;
	if (ws.length <= off) return null;
	const w = ws[off];
	let m = reClock24.exec(w);
	if (m) return { time: `${pad(Number(m[1]))}:${m[2]}`, n: off + 1 };
	m = reClock12.exec(w);
	if (m) return { time: clock12(m[1], m[2], m[3]), n: off + 1 };
	m = reBareHour.exec(w);
	if (m && (ws[off + 1] === 'am' || ws[off + 1] === 'pm')) return { time: clock12(m[1], m[2], ws[off + 1]), n: off + 2 };
	return null;
}

function lowerFrom(words: Word[], i: number): string[] {
	const out: string[] = [];
	for (let k = i; k < words.length && k < i + 4; k++) {
		if (words[k].escaped) break;
		out.push(words[k].text.toLowerCase());
	}
	return out;
}

export function parseQuickAdd(input: string, ctx: QAContext): QAResult {
	const today = todayIn(ctx.now, ctx.tz);
	const weekStart = ctx.weekStart === 'sunday' ? 0 : 1;
	const words = split(input);
	const used = new Array<boolean>(words.length).fill(false);
	const res: QAResult = { title: '', labels: [], labelsNew: [], tokens: [] };
	let columnWord = -1;
	let project: QAProject | undefined;

	const tok = (kind: TokenKind, from: number, to: number, value: string) => {
		res.tokens.push({ kind, text: input.slice(words[from].start, words[to].end), start: words[from].start, end: words[to].end, value });
		for (let k = from; k <= to; k++) used[k] = true;
	};
	const findProject = (q: string) => {
		const name = matchName(ctx.projects.map((p) => p.name), q);
		return name ? ctx.projects.find((p) => p.name === name) : undefined;
	};

	for (let i = 0; i < words.length; i++) {
		const w = words[i];
		if (w.escaped || used[i] || w.text.length < 2) continue;
		const lw = w.text.toLowerCase();
		const c0 = w.text[0];
		if (c0 === '#' && !res.project && !res.projectNew) {
			let name = w.text.slice(1);
			if (name.startsWith('"') && name.endsWith('"') && name.length >= 2) name = name.slice(1, -1);
			else if (!reName.test(name)) continue;
			if (!name) continue;
			const p = findProject(name);
			if (p) {
				project = p;
				res.project = p.name;
				tok('project', i, i, p.name);
			} else {
				res.projectNew = name;
				tok('project', i, i, name);
			}
			continue;
		}
		if (c0 === '/' && columnWord < 0 && reName.test(w.text.slice(1))) {
			columnWord = i;
			continue;
		}
		if (c0 === '@' && reName.test(w.text.slice(1))) {
			const name = w.text.slice(1);
			const canon = matchName(ctx.labels, name);
			if (canon) {
				res.labels.push(canon);
				tok('label', i, i, canon);
			} else {
				res.labelsNew.push(name);
				tok('label', i, i, name);
			}
			continue;
		}
		if (c0 === '!' && res.priority === undefined) {
			const p = priorities[lw.slice(1)];
			if (p !== undefined) {
				res.priority = p;
				tok('priority', i, i, String(p));
			}
			continue;
		}
		if (res.dueDate) continue;
		if (lw.startsWith('due:')) {
			const rest = lw.slice(4);
			let seq = [rest, ...words.slice(i + 1, i + 4).map((x) => x.text.toLowerCase())];
			if (!rest) seq = seq.slice(1);
			const d = matchDate(seq, today, weekStart);
			if (d) {
				let last = rest ? i + d.n - 1 : i + d.n;
				let time = d.time;
				const t = matchTime(lowerFrom(words, last + 1));
				if (t) {
					time = t.time;
					last += t.n;
				}
				res.dueDate = d.date;
				if (time) res.dueTime = time;
				tok('date', i, last, d.date);
			}
			continue;
		}
		const d = matchDate(lowerFrom(words, i), today, weekStart);
		if (d && !used.slice(i, i + d.n).some(Boolean)) {
			let last = i + d.n - 1;
			let time = d.time;
			const t = matchTime(lowerFrom(words, last + 1));
			if (t) {
				time = t.time;
				last += t.n;
			}
			res.dueDate = d.date;
			if (time) res.dueTime = time;
			tok('date', i, last, d.date);
			i = last;
		}
	}

	if (columnWord >= 0) {
		let target = project;
		if (!target && !res.projectNew && ctx.currentProject) {
			target = findProject(ctx.currentProject);
			if (target) res.project = target.name;
		}
		if (target) {
			const canon = matchName(target.columns, words[columnWord].text.slice(1));
			if (canon) {
				res.column = canon;
				tok('column', columnWord, columnWord, canon);
			}
		} else if (res.projectNew) {
			res.columnNew = words[columnWord].text.slice(1);
			tok('column', columnWord, columnWord, res.columnNew);
		}
	}

	res.title = words.filter((_, i) => !used[i]).map((w) => w.text).join(' ');
	if (!res.title.trim()) res.error = 'empty_title';
	res.tokens.sort((a, b) => a.start - b.start);
	return res;
}
