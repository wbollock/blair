import { todayIn } from './quickadd.ts';

export function today(tz: string): string {
	return todayIn(new Date(), tz).toString();
}

export function addDays(date: string, n: number): string {
	const [y, m, d] = date.split('-').map(Number);
	const t = new Date(Date.UTC(y, m - 1, d + n));
	return t.toISOString().slice(0, 10);
}

function weekday(date: string): number {
	const [y, m, d] = date.split('-').map(Number);
	return new Date(Date.UTC(y, m - 1, d)).getUTCDay();
}

/** Upcoming Saturday (or today if it's the weekend). */
export function thisWeekend(tz: string): string {
	const t = today(tz);
	const wd = weekday(t);
	return wd === 6 || wd === 0 ? t : addDays(t, 6 - wd);
}

/** Next Monday (or Sunday for sunday-start weeks). */
export function nextWeek(tz: string, weekStart: string): string {
	const t = today(tz);
	const start = weekStart === 'sunday' ? 0 : 1;
	const diff = ((start - weekday(t) + 7) % 7) || 7;
	return addDays(t, diff);
}

const fmt = (date: string, opts: Intl.DateTimeFormatOptions) => {
	const [y, m, d] = date.split('-').map(Number);
	return new Intl.DateTimeFormat(undefined, { ...opts, timeZone: 'UTC' }).format(new Date(Date.UTC(y, m - 1, d)));
};

export function formatTime(time: string): string {
	const [h, m] = time.split(':').map(Number);
	const d = new Date(Date.UTC(2000, 0, 1, h, m));
	return new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: m ? '2-digit' : undefined, timeZone: 'UTC' }).format(d);
}

/** Short relative label: Today, Tomorrow, Yesterday, Fri, Mon 3 Dec, 3 Dec 2027. */
export function dueLabel(date: string, time: string | null | undefined, tz: string): string {
	const t = today(tz);
	let label: string;
	if (date === t) label = 'Today';
	else if (date === addDays(t, 1)) label = 'Tomorrow';
	else if (date === addDays(t, -1)) label = 'Yesterday';
	else if (date > t && date <= addDays(t, 6)) label = fmt(date, { weekday: 'short' });
	else if (date.slice(0, 4) === t.slice(0, 4)) label = fmt(date, { weekday: 'short', day: 'numeric', month: 'short' });
	else label = fmt(date, { day: 'numeric', month: 'short', year: 'numeric' });
	return time ? `${label} ${formatTime(time)}` : label;
}

export function longDate(date: string): string {
	return fmt(date, { weekday: 'long', month: 'long', day: 'numeric' });
}

export function relativeTime(iso: string): string {
	const s = (Date.now() - new Date(iso).getTime()) / 1000;
	if (s < 60) return 'just now';
	if (s < 3600) return `${Math.floor(s / 60)} min ago`;
	if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
	return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(iso));
}

/** Milliseconds the zone is ahead of UTC at the given instant. */
function zoneOffset(ms: number, tz: string): number {
	const parts = new Intl.DateTimeFormat('en-US', {
		timeZone: tz, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit'
	}).formatToParts(new Date(ms));
	const get = (type: string) => Number(parts.find((p) => p.type === type)?.value);
	return Date.UTC(get('year'), get('month') - 1, get('day'), get('hour'), get('minute'), get('second')) - Math.floor(ms / 1000) * 1000;
}

/** A wall-clock date and time in `tz` as a UTC ISO timestamp. */
export function zonedToISO(date: string, time: string, tz: string): string {
	const [y, m, d] = date.split('-').map(Number);
	const [h, mi] = time.split(':').map(Number);
	const wall = Date.UTC(y, m - 1, d, h, mi);
	let t = wall - zoneOffset(wall, tz);
	t = wall - zoneOffset(t, tz); // second pass settles DST transitions
	return new Date(t).toISOString();
}

/** A UTC timestamp as wall-clock date and time in `tz`. */
export function isoToZoned(iso: string, tz: string): { date: string; time: string } {
	const ms = new Date(iso).getTime();
	const local = new Date(ms + zoneOffset(ms, tz)).toISOString();
	return { date: local.slice(0, 10), time: local.slice(11, 16) };
}

/** "Tomorrow 9 AM" for a reminder timestamp. */
export function remindLabel(iso: string, tz: string): string {
	const { date, time } = isoToZoned(iso, tz);
	return dueLabel(date, time, tz);
}
