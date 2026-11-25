import { describe, expect, it } from 'vitest';
import { isoToZoned, zonedToISO } from './dates.ts';

describe('zoned times', () => {
	it('converts wall-clock times in a zone to UTC and back', () => {
		expect(zonedToISO('2026-11-24', '09:00', 'America/New_York')).toBe('2026-11-24T14:00:00.000Z');
		expect(zonedToISO('2026-07-01', '09:00', 'America/New_York')).toBe('2026-07-01T13:00:00.000Z');
		expect(zonedToISO('2026-11-24', '09:00', 'Asia/Kolkata')).toBe('2026-11-24T03:30:00.000Z');
		expect(isoToZoned('2026-11-25T03:30:00.000Z', 'America/New_York')).toEqual({ date: '2026-11-24', time: '22:30' });
	});
	it('handles the day DST starts', () => {
		// 2026-03-08 03:30 EDT (UTC-4) = 07:30Z
		expect(zonedToISO('2026-03-08', '03:30', 'America/New_York')).toBe('2026-03-08T07:30:00.000Z');
	});
});
