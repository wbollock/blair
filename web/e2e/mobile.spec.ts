import { expect, test } from '@playwright/test';
import { EMAIL, PASSWORD, loadState, saveState, totp } from './helpers';

const shots = process.env.SCREENSHOT_DIR;

test('mobile: sign in, capture sheet keeps the task in reach', async ({ page }) => {
	const st = loadState();
	await page.goto('/login');
	await page.getByLabel('Email').fill(EMAIL);
	await page.getByLabel('Password').fill(PASSWORD);
	await page.getByRole('button', { name: 'Sign in' }).click();
	const c = await totp(st.secret, st.lastStep);
	await page.getByLabel('Authentication code').fill(c.code);
	await page.getByRole('button', { name: 'Verify' }).click();
	saveState({ ...st, lastStep: c.step });
	await expect(page).toHaveURL(/\/today/);
	if (shots) await page.screenshot({ path: `${shots}/mobile-today.png` });

	await page.getByRole('button', { name: 'Quick capture' }).click();
	const sheet = page.getByRole('dialog', { name: 'Quick capture' });
	await sheet.getByLabel('New task').fill('Buy **milk** and coffee');
	await sheet.getByLabel('New task').press('Enter');
	const card = sheet.locator('section[aria-label="Just added"] article').first();
	await expect(card).toContainText('Buy milk and coffee');
	// Sheet stays open with the input ready for the next one.
	await expect(sheet.getByLabel('New task')).toBeFocused();
	await card.getByRole('button', { name: 'Due date' }).click();
	await page.getByRole('menuitem', { name: /^Today/ }).click();
	await expect(card.getByRole('button', { name: 'Due date' })).toContainText('Today');
	if (shots) await page.screenshot({ path: `${shots}/mobile-capture.png` });

	await sheet.getByRole('tab', { name: 'Note' }).click();
	await sheet.locator('textarea').fill('Idea: show quick-add hints in the empty inbox');
	await sheet.getByRole('button', { name: 'Save note' }).click();
	await expect(page.getByText('Note saved')).toBeVisible();
});
