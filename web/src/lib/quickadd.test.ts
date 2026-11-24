import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { parseQuickAdd, type QAContext } from './quickadd.ts';

const vectors = JSON.parse(readFileSync(new URL('../../../testdata/quickadd/cases.json', import.meta.url), 'utf8'));
const base: QAContext = {
	now: new Date(vectors.context.now),
	tz: vectors.context.tz,
	weekStart: vectors.context.week_start,
	projects: vectors.context.projects,
	labels: vectors.context.labels
};

describe('shared quick-add vectors', () => {
	for (const c of vectors.cases) {
		it(c.name, () => {
			const r = parseQuickAdd(c.input, { ...base, currentProject: c.context_project });
			const e = c.expect;
			if (e.error) {
				expect(r.error).toBe(e.error);
				return;
			}
			expect(r.error).toBeUndefined();
			expect(r.title).toBe(e.title);
			expect(r.project ?? '').toBe(e.project ?? '');
			expect(r.projectNew ?? '').toBe(e.project_new ?? '');
			expect(r.column ?? '').toBe(e.column ?? '');
			expect(r.columnNew ?? '').toBe(e.column_new ?? '');
			expect(r.priority).toBe(e.priority);
			expect(r.dueDate ?? '').toBe(e.due_date ?? '');
			expect(r.dueTime ?? '').toBe(e.due_time ?? '');
			expect(r.labels).toEqual(e.labels ?? []);
			for (const t of r.tokens) expect(c.input.slice(t.start, t.end)).toBe(t.text);
		});
	}
});

describe('robustness', () => {
	it('never returns an empty title without an error', () => {
		const alphabet = ['#', '@', '!', '/', '\\', '"', ' ', 'a', '1', 'fri', 'tomorrow', 'at', '9am', 'next', 'in', '2w', 'é'];
		let seed = 42;
		const rand = () => (seed = (seed * 1103515245 + 12345) % 2 ** 31) / 2 ** 31;
		for (let n = 0; n < 5000; n++) {
			let s = '';
			const len = Math.floor(rand() * 12);
			for (let k = 0; k < len; k++) s += alphabet[Math.floor(rand() * alphabet.length)] + (rand() < 0.5 ? ' ' : '');
			const r = parseQuickAdd(s, base);
			if (!r.error) expect(r.title.trim()).not.toBe('');
			for (const t of r.tokens) expect(s.slice(t.start, t.end)).toBe(t.text);
		}
	});
});
