import { describe, expect, it } from 'vitest';
import { fuzzy, rank } from './fuzzy.ts';

describe('fuzzy', () => {
	it('matches characters in order, case-insensitively', () => {
		expect(fuzzy('rwc', 'Renew wildcard cert')?.hits).toHaveLength(3);
		expect(fuzzy('xyz', 'Renew wildcard cert')).toBeNull();
		expect(fuzzy('CERT', 'renew cert')).not.toBeNull();
	});
	it('ranks substrings and word starts above scattered matches', () => {
		const items = ['Order furnace filter', 'Fix gutter', 'Draft post: why I left NPM', 'Fujifilm lens'];
		expect(rank('fi', items, (s) => s).map((r) => r.item)[0]).toBe('Fix gutter');
		expect(rank('gut', items, (s) => s)[0].item).toBe('Fix gutter');
		expect(rank('ff', items, (s) => s)[0].item).toBe('Order furnace filter');
	});
	it('returns everything for an empty query', () => {
		expect(rank('', ['a', 'b'], (s) => s)).toHaveLength(2);
	});
});
