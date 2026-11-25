import { describe, expect, it } from 'vitest';
import { firstUrl, linkedTitle, standup } from './standup.ts';

describe('standup', () => {
	it('links titles to the first URL in their notes', () => {
		expect(firstUrl('see https://b.example and [x](https://a.example)')).toBe('https://b.example');
		expect(firstUrl('<a href="https://h.example/?a=1&amp;b=2">h</a>')).toBe('https://h.example/?a=1&b=2');
		expect(linkedTitle('Fix CI', 'PR: https://github.com/o/r/pull/9')).toBe('[Fix CI](https://github.com/o/r/pull/9)');
		expect(linkedTitle('[Already](https://x.example)', 'https://y.example')).toBe('[Already](https://x.example)');
		expect(linkedTitle('No link', '')).toBe('No link');
	});
	it('formats plain text and escaped HTML', () => {
		const s = standup([
			{ title: 'Fix CI', description: 'https://ci.example/1' },
			{ title: 'Ship <b>it</b>', description: '' }
		]);
		expect(s.plain).toBe('*Tasks*\n• [Fix CI](https://ci.example/1)\n• Ship <b>it</b>');
		expect(s.html).toContain('<a href="https://ci.example/1">Fix CI</a>');
		expect(s.html).toContain('Ship &lt;b&gt;it&lt;/b&gt;');
	});
});
