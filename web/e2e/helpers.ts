import { createHmac } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import type { Page } from '@playwright/test';

export const EMAIL = 'will@example.com';
export const PASSWORD = 'plum-orbit-kettle-91';
const STATE = '/tmp/blair-e2e-state.json';

export function saveState(s: Record<string, unknown>) {
	writeFileSync(STATE, JSON.stringify(s));
}
export function loadState(): { secret: string; lastStep: number } {
	return JSON.parse(readFileSync(STATE, 'utf8'));
}

function base32(s: string): Buffer {
	const alpha = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
	let bits = '';
	for (const c of s.replace(/=+$/, '').toUpperCase()) bits += alpha.indexOf(c).toString(2).padStart(5, '0');
	const out = [];
	for (let i = 0; i + 8 <= bits.length; i += 8) out.push(parseInt(bits.slice(i, i + 8), 2));
	return Buffer.from(out);
}

/** RFC 6238 code for the current step, waiting for a fresh step if `after` was already used. */
export async function totp(secret: string, after = 0): Promise<{ code: string; step: number }> {
	let step = Math.floor(Date.now() / 30000);
	while (step <= after) {
		await new Promise((r) => setTimeout(r, 1000));
		step = Math.floor(Date.now() / 30000);
	}
	const msg = Buffer.alloc(8);
	msg.writeBigUInt64BE(BigInt(step));
	const h = createHmac('sha1', base32(secret)).update(msg).digest();
	const off = h[h.length - 1] & 0xf;
	const v = (h.readUInt32BE(off) & 0x7fffffff) % 1_000_000;
	return { code: String(v).padStart(6, '0'), step };
}

export async function setTheme(page: Page, theme: 'light' | 'dark') {
	await page.evaluate((t) => (document.documentElement.dataset.theme = t), theme);
}
