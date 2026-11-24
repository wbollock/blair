import { expect, test } from '@playwright/test';
import { EMAIL, PASSWORD, saveState, setTheme, totp } from './helpers';

test.describe.configure({ mode: 'serial' });
const shots = process.env.SCREENSHOT_DIR;

test('setup, 2FA, quick-add, tray, quick actions, board', async ({ page }) => {
	const setupUrl = process.env.SETUP_URL;
	test.skip(!setupUrl, 'SETUP_URL not set');

	// First-run setup creates the admin and lands on security settings.
	await page.goto(setupUrl!.replace(/^https?:\/\/[^/]+/, ''));
	await page.getByLabel('Your name').fill('Will');
	await page.getByLabel('Email').fill(EMAIL);
	await page.getByLabel(/^Password/).fill(PASSWORD);
	await page.getByLabel('Confirm password').fill(PASSWORD + 'x');
	await expect(page.getByRole('button', { name: 'Create account' })).toBeDisabled();
	await page.getByLabel('Confirm password').fill(PASSWORD);
	await page.getByRole('button', { name: 'Create account' }).click();
	await expect(page.getByText('Turn on two-factor authentication now')).toBeVisible();

	// Enable TOTP.
	await page.getByRole('button', { name: 'Set up 2FA' }).click();
	const secret = (await page.locator('code.secret').textContent())!.trim();
	const first = await totp(secret);
	await page.getByLabel('Then enter the 6-digit code').fill(first.code);
	await page.getByRole('button', { name: 'Turn on' }).click();
	await expect(page.getByText('Save these recovery codes')).toBeVisible();
	await page.getByRole('button', { name: "I've saved them" }).click();
	await expect(page.getByText(/On · 10 recovery codes left/)).toBeVisible();

	// Sign out and back in with password + TOTP.
	await page.getByRole('tab', { name: 'Profile' }).click();
	await page.getByRole('button', { name: 'Sign out' }).click();
	await expect(page).toHaveURL(/\/login/);
	await page.getByLabel('Email').fill(EMAIL);
	await page.getByLabel('Password').fill(PASSWORD);
	await page.getByRole('button', { name: 'Sign in' }).click();
	const second = await totp(secret, first.step);
	await page.getByLabel('Authentication code').fill(second.code);
	await page.getByRole('button', { name: 'Verify' }).click();
	await expect(page).toHaveURL(/\/today/);
	saveState({ secret, lastStep: second.step });

	// Quick-add with syntax creates the project, label and due date (SPEC §5.5 #4).
	const qa = page.getByLabel('New task', { exact: true });
	await qa.fill('Fix gutter #homelab /doing !high fri at 9am @house');
	await qa.press('Enter');
	const card = page.locator('section[aria-label="Just added"] article').first();
	await expect(card).toContainText('Fix gutter');
	await expect(card.getByRole('button', { name: 'Project' })).toContainText('homelab');
	await expect(card.getByRole('button', { name: 'Column' })).toContainText('Doing');
	// The task isn't due today, so the tray explains where it went (§5.5 #3).
	await expect(card).toContainText('not shown in Today');
	await expect(qa).toBeFocused();

	// A plain task: file it from the tray with chips (§5.5 #2).
	await qa.fill('Renew wildcard cert for `*.wboll.dev`');
	await qa.press('Enter');
	const card2 = page.locator('section[aria-label="Just added"] article').first();
	await expect(card2).toContainText('Renew wildcard cert');
	await card2.getByRole('button', { name: 'Due date' }).click();
	await page.getByRole('menuitem', { name: /^Today/ }).click();
	await expect(page.locator('[role=list] [data-task-id]').filter({ hasText: 'Renew wildcard cert' })).toBeVisible();
	await card2.getByRole('button', { name: 'Priority' }).click();
	await page.getByRole('menuitem', { name: 'Urgent' }).click();
	await expect(card2.getByRole('button', { name: 'Priority' })).toContainText('Urgent');

	// More tasks for a realistic screenshot.
	for (const t of ['Change **tududi** admin password today !urgent #homelab', 'Draft post: _why I left NPM for Caddy_ #blog-ideas today 5pm !med', 'Order furnace filter today @house !low', 'Back up `caddy/data` yesterday #homelab !high']) {
		await qa.fill(t);
		await qa.press('Enter');
	}
	await expect(page.getByText('Overdue')).toBeVisible();

	// Right-click quick actions: move to a column via the submenu (§5.5 #7).
	const row = page.locator('[role=list] [data-task-id]').filter({ hasText: 'Renew wildcard cert' });
	await row.click({ button: 'right' });
	await expect(page.getByRole('menu', { name: 'Quick actions' })).toBeVisible();
	await page.getByRole('menuitem', { name: 'Move to project' }).click();
	await page.getByRole('menuitem', { name: 'homelab' }).click();
	await expect(row).toContainText('homelab · To do');
	await expect(page.getByText('Moved to homelab')).toBeVisible();

	if (shots) {
		await qa.fill('Call dentist tomorrow 9am @errands');
		await page.locator('h1').click();
		await setTheme(page, 'light');
		await page.screenshot({ path: `${shots}/today-light.png` });
		await row.click({ button: 'right' });
		await page.getByRole('menuitem', { name: 'Column' }).hover();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${shots}/quick-actions.png` });
		await page.keyboard.press('Escape');
		await page.keyboard.press('Escape');
		await setTheme(page, 'dark');
		await page.screenshot({ path: `${shots}/today-dark.png` });
		await setTheme(page, 'light');
	}

	// Board view: drag-free move via quick actions, then check the column.
	await page.getByRole('link', { name: /homelab/ }).first().click();
	await page.getByRole('button', { name: 'Board' }).click();
	const board = page.getByRole('list', { name: 'Board' });
	await expect(board.getByRole('region', { name: 'Doing' })).toContainText('Fix gutter');
	await board.getByRole('region', { name: 'To do' }).getByRole('button', { name: 'Add task' }).click();
	await page.getByLabel('New task in To do').fill('Replace UPS battery !med');
	await page.getByLabel('New task in To do').press('Enter');
	await expect(board.getByRole('region', { name: 'To do' })).toContainText('Replace UPS battery');
	if (shots) await page.screenshot({ path: `${shots}/board.png` });

	// Drag a just-added card from another project straight onto a board column.
	await qa.fill('Call plumber #blog-ideas !low');
	await qa.press('Enter');
	const plumber = page.locator('section[aria-label="Just added"] article').filter({ hasText: 'Call plumber' });
	await expect(plumber).toContainText('not shown in homelab');
	await plumber.dragTo(board.getByRole('region', { name: 'Doing' }));
	await expect(board.getByRole('region', { name: 'Doing' })).toContainText('Call plumber');
	await expect(plumber.getByRole('button', { name: 'Project' })).toContainText('homelab');
	// Switch kanban status from the tray: Done moves it to the Done column.
	await plumber.getByRole('button', { name: 'Status' }).click();
	await page.getByRole('menuitem', { name: /^Done/ }).click();
	await expect(board.getByRole('region', { name: 'Done' })).toContainText('Call plumber');
	// Drop a board card onto a sidebar project to move it there.
	await board.locator('[data-task-id]').filter({ hasText: 'Replace UPS battery' }).dragTo(page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: /blog-ideas/ }));
	await expect(board).not.toContainText('Replace UPS battery');
	await expect(page.getByText('Moved to blog-ideas')).toBeVisible();

	// Task drawer edits and markdown.
	await board.getByText('Fix gutter').click();
	const drawer = page.getByRole('complementary', { name: 'Task details' });
	await expect(drawer).toBeVisible();
	await drawer.getByRole('button', { name: 'Add notes…' }).click();
	await drawer.locator('textarea').fill('- [ ] buy brackets\n- [x] borrow ladder\n\nUse the **tall** ladder.');
	await drawer.getByRole('button', { name: 'Done' }).click();
	await drawer.getByLabel('New checklist item').fill('Clear leaves');
	await drawer.getByLabel('New checklist item').press('Enter');
	await expect(drawer).toContainText('Checklist 0/1');
	if (shots) await page.screenshot({ path: `${shots}/drawer.png` });
	await page.keyboard.press('Escape');

	// API docs render.
	const docs = await page.request.get('/api/openapi.json');
	expect(docs.ok()).toBeTruthy();
});
