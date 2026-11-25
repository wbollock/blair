// Repeat rules: the RRULE subset in internal/recur (ADR-0013). Both must pass
// testdata/recur/next.json. Dates are YYYY-MM-DD strings.

export interface Rule {
	freq: 'DAILY' | 'WEEKLY' | 'MONTHLY' | 'YEARLY';
	interval: number;
	/** Weekdays, 0 = Sunday, sorted Monday first. WEEKLY only. */
	byDay: number[];
	/** 1–31, clamped to short months. MONTHLY only. */
	byMonthDay: number;
}

const CODES = ['SU', 'MO', 'TU', 'WE', 'TH', 'FR', 'SA'];
const FREQS = ['DAILY', 'WEEKLY', 'MONTHLY', 'YEARLY'] as const;
const monFirst = (d: number) => (d + 6) % 7;
const sortDays = (days: number[]) => [...new Set(days)].sort((a, b) => monFirst(a) - monFirst(b));

export function parseRule(s: string): Rule {
	const r: Rule = { freq: 'DAILY', interval: 1, byDay: [], byMonthDay: 0 };
	let freq = '';
	for (const part of s.trim().toUpperCase().split(';')) {
		const [k, v] = part.split('=');
		if (v === undefined) throw new Error(`bad rule part ${part}`);
		if (k === 'FREQ') {
			if (!(FREQS as readonly string[]).includes(v)) throw new Error('bad FREQ');
			freq = v;
		} else if (k === 'INTERVAL') {
			const n = Number(v);
			if (!Number.isInteger(n) || n < 1 || n > 365) throw new Error('bad INTERVAL');
			r.interval = n;
		} else if (k === 'BYDAY') {
			r.byDay = sortDays(
				v.split(',').map((c) => {
					const i = CODES.indexOf(c);
					if (i < 0) throw new Error(`bad BYDAY ${c}`);
					return i;
				})
			);
		} else if (k === 'BYMONTHDAY') {
			const n = Number(v);
			if (!Number.isInteger(n) || n < 1 || n > 31) throw new Error('bad BYMONTHDAY');
			r.byMonthDay = n;
		} else throw new Error(`unsupported rule part ${k}`);
	}
	if (!freq) throw new Error('FREQ is required');
	r.freq = freq as Rule['freq'];
	if (r.byDay.length && r.freq !== 'WEEKLY') throw new Error('BYDAY needs FREQ=WEEKLY');
	if (r.byMonthDay && r.freq !== 'MONTHLY') throw new Error('BYMONTHDAY needs FREQ=MONTHLY');
	return r;
}

export function ruleString(r: Rule): string {
	let s = `FREQ=${r.freq}`;
	if (r.interval > 1) s += `;INTERVAL=${r.interval}`;
	if (r.byDay.length) s += `;BYDAY=${r.byDay.map((d) => CODES[d]).join(',')}`;
	if (r.byMonthDay) s += `;BYMONTHDAY=${r.byMonthDay}`;
	return s;
}

const toUTC = (d: string) => {
	const [y, m, day] = d.split('-').map(Number);
	return new Date(Date.UTC(y, m - 1, day));
};
const fmt = (t: Date) => t.toISOString().slice(0, 10);
const addDays = (t: Date, n: number) => new Date(Date.UTC(t.getUTCFullYear(), t.getUTCMonth(), t.getUTCDate() + n));
function clampDay(y: number, m0: number, d: number): Date {
	const last = new Date(Date.UTC(y, m0 + 1, 0)).getUTCDate();
	return new Date(Date.UTC(y, m0, Math.min(d, last)));
}
const addMonths = (t: Date, n: number, day: number) => {
	const first = new Date(Date.UTC(t.getUTCFullYear(), t.getUTCMonth() + n, 1));
	return clampDay(first.getUTCFullYear(), first.getUTCMonth(), day);
};

/** First occurrence strictly after `from`, or on/after it when inclusive. */
export function nextDate(r: Rule, from: string, inclusive: boolean): string {
	const f = toUTC(from);
	const iv = Math.max(r.interval, 1);
	switch (r.freq) {
		case 'DAILY':
			return fmt(inclusive ? f : addDays(f, iv));
		case 'WEEKLY': {
			if (!r.byDay.length) return fmt(inclusive ? f : addDays(f, 7 * iv));
			const start = inclusive ? f : addDays(f, 1);
			const on = (t: Date) => r.byDay.includes(t.getUTCDay());
			if (iv === 1) {
				for (let k = 0; k < 7; k++) if (on(addDays(start, k))) return fmt(addDays(start, k));
			}
			const week = addDays(f, -monFirst(f.getUTCDay()));
			for (let t = start; t < addDays(week, 7); t = addDays(t, 1)) if (on(t)) return fmt(t);
			const next = addDays(week, 7 * iv);
			for (let k = 0; k < 7; k++) if (on(addDays(next, k))) return fmt(addDays(next, k));
			return fmt(f);
		}
		case 'MONTHLY': {
			if (!r.byMonthDay) return fmt(inclusive ? f : addMonths(f, iv, f.getUTCDate()));
			const c = clampDay(f.getUTCFullYear(), f.getUTCMonth(), r.byMonthDay);
			if (c > f || (inclusive && c.getTime() === f.getTime())) return fmt(c);
			return fmt(addMonths(f, iv, r.byMonthDay));
		}
		case 'YEARLY':
			return fmt(inclusive ? f : addMonths(f, 12 * iv, f.getUTCDate()));
	}
}

const WEEKDAY_WORDS: Record<string, number> = {
	sun: 0, sunday: 0, mon: 1, monday: 1, tue: 2, tues: 2, tuesday: 2, wed: 3, weds: 3, wednesday: 3,
	thu: 4, thur: 4, thurs: 4, thursday: 4, fri: 5, friday: 5, sat: 6, saturday: 6
};
const UNITS: Record<string, Rule['freq']> = {
	day: 'DAILY', days: 'DAILY', week: 'WEEKLY', weeks: 'WEEKLY', month: 'MONTHLY', months: 'MONTHLY', year: 'YEARLY', years: 'YEARLY'
};

/** Reads the words after "every" (lowercase). n = 0 when they aren't a schedule. Mirrors recur.Phrase. */
export function phrase(words: string[]): { rule: Rule; n: number } {
	const none = { rule: { freq: 'DAILY', interval: 1, byDay: [], byMonthDay: 0 } as Rule, n: 0 };
	const w = words[0];
	if (!w) return none;
	const mk = (freq: Rule['freq'], interval = 1, byDay: number[] = [], byMonthDay = 0, n = 1) => ({ rule: { freq, interval, byDay, byMonthDay }, n });
	if (w === 'weekday' || w === 'weekdays') return mk('WEEKLY', 1, [1, 2, 3, 4, 5]);
	if (UNITS[w] && !w.endsWith('s')) return mk(UNITS[w]);
	if (words.length > 1 && UNITS[words[1]]) {
		const unit = UNITS[words[1]];
		const plural = words[1].endsWith('s');
		if (w === 'other' && !plural) return mk(unit, 2, [], 0, 2);
		const n = /^\d+$/.test(w) ? Number(w) : NaN;
		if (n >= 1 && n <= 365 && (n === 1) === !plural) return mk(unit, n, [], 0, 2);
	}
	const parts = w.split(',');
	if (parts.every((p) => p in WEEKDAY_WORDS)) return mk('WEEKLY', 1, sortDays(parts.map((p) => WEEKDAY_WORDS[p])));
	const m = /^(\d{1,2})(st|nd|rd|th)$/.exec(w);
	if (m && Number(m[1]) >= 1 && Number(m[1]) <= 31) return mk('MONTHLY', 1, [], Number(m[1]));
	return none;
}

const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const ordinal = (n: number) => n + (n % 10 === 1 && n !== 11 ? 'st' : n % 10 === 2 && n !== 12 ? 'nd' : n % 10 === 3 && n !== 13 ? 'rd' : 'th');

/** "Every Mon, Thu", "Every 2 weeks", "Every month on the 15th". */
export function ruleLabel(s: string): string {
	let r: Rule;
	try {
		r = parseRule(s);
	} catch {
		return 'Repeats';
	}
	const unit = { DAILY: 'day', WEEKLY: 'week', MONTHLY: 'month', YEARLY: 'year' }[r.freq];
	let out = r.interval > 1 ? `Every ${r.interval} ${unit}s` : `Every ${unit}`;
	if (r.byDay.length) {
		const weekdays = r.byDay.length === 5 && [1, 2, 3, 4, 5].every((d) => r.byDay.includes(d));
		out = weekdays && r.interval === 1 ? 'Every weekday' : `${r.interval > 1 ? out + ' on' : 'Every'} ${r.byDay.map((d) => DAY_NAMES[d]).join(', ')}`;
	}
	if (r.byMonthDay) out += ` on the ${ordinal(r.byMonthDay)}`;
	return out;
}
