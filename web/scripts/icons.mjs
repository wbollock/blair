// Renders PWA icons from static/icon.svg (run: node scripts/icons.mjs).
import { chromium } from '@playwright/test';
import { readFileSync } from 'node:fs';

const svg = readFileSync(new URL('../static/icon.svg', import.meta.url), 'utf8');
const browser = await chromium.launch();
const page = await browser.newPage();
const shots = [
	['icon-192.png', 192, 0, false],
	['icon-512.png', 512, 0, false],
	['icon-maskable-512.png', 512, 0.1, true],
	['apple-touch-icon.png', 180, 0, true]
];
for (const [name, size, pad, square] of shots) {
	await page.setViewportSize({ width: size, height: size });
	const inner = square ? svg.replace('rx="112"', 'rx="0"') : svg;
	await page.setContent(`<html><body style="margin:0;background:${square ? '#B84A33' : 'transparent'}"><div style="padding:${size * pad}px;width:${size}px;height:${size}px;box-sizing:border-box">${inner.replace('<svg ', `<svg width="${size - 2 * size * pad}" height="${size - 2 * size * pad}" `)}</div></body></html>`);
	await page.screenshot({ path: new URL(`../static/${name}`, import.meta.url).pathname, omitBackground: !square });
}
await browser.close();
