import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { nextDate, parseRule, phrase, ruleLabel, ruleString } from './recur.ts';

const v = JSON.parse(readFileSync(new URL('../../../testdata/recur/next.json', import.meta.url), 'utf8'));

describe('shared repeat vectors', () => {
	for (const c of v.next) {
		it(`next(${c.rule}, ${c.from}, ${c.inclusive})`, () => expect(nextDate(parseRule(c.rule), c.from, c.inclusive)).toBe(c.want));
	}
	for (const c of v.canonical) {
		it(`canonical ${c.in}`, () => {
			if (c.error) expect(() => parseRule(c.in)).toThrow();
			else expect(ruleString(parseRule(c.in))).toBe(c.want);
		});
	}
});

describe('phrases and labels', () => {
	it('reads schedules the same way as the Go parser', () => {
		const p = (s: string) => {
			const r = phrase(s.split(' '));
			return r.n ? ruleString(r.rule) : '';
		};
		expect(p('other week')).toBe('FREQ=WEEKLY;INTERVAL=2');
		expect(p('fri,mon')).toBe('FREQ=WEEKLY;BYDAY=MO,FR');
		expect(p('3 month')).toBe('');
		expect(p('box')).toBe('');
	});
	it('labels rules for people', () => {
		expect(ruleLabel('FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR')).toBe('Every weekday');
		expect(ruleLabel('FREQ=WEEKLY;INTERVAL=2;BYDAY=MO')).toBe('Every 2 weeks on Mon');
		expect(ruleLabel('FREQ=MONTHLY;BYMONTHDAY=22')).toBe('Every month on the 22nd');
		expect(ruleLabel('FREQ=DAILY')).toBe('Every day');
	});
});
