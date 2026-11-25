<script lang="ts">
	import { page } from '$app/state';
	import QRCode from 'qrcode';
	import { api, ApiError, ok, type Schemas } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import { relativeTime } from '#lib/dates.ts';
	import ViewHeader from '#lib/components/ViewHeader.svelte';

	type Tab = 'profile' | 'security' | 'tokens' | 'data' | 'admin';
	let tab = $state<Tab>((page.url.searchParams.get('tab') as Tab) || 'profile');
	const welcome = page.url.searchParams.get('welcome') === '1';
	const tabs = $derived<[Tab, string][]>([
		['profile', 'Profile'],
		['security', 'Security'],
		['tokens', 'API tokens'],
		['data', 'Data'],
		...(app.me?.is_admin ? [['admin', 'Admin'] as [Tab, string]] : [])
	]);
	const err = (e: unknown) => (e instanceof ApiError ? e.problem.errors?.[0]?.message || e.message : 'Something went wrong');

	// ---- profile ----
	let name = $state(app.me?.display_name ?? '');
	let tz = $state(app.me?.timezone ?? 'UTC');
	let weekStart = $state(app.me?.week_start ?? 'monday');
	const zones = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : [app.me?.timezone ?? 'UTC'];
	async function saveProfile() {
		try {
			app.me = await ok(api.PATCH('/me', { body: { display_name: name, timezone: tz, week_start: weekStart } }));
			app.toast('Profile saved');
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}

	// ---- security ----
	let curPw = $state('');
	let newPw = $state('');
	let newPw2 = $state('');
	let totp = $state<Schemas['TOTPSetup'] | null>(null);
	let qr = $state('');
	let code = $state('');
	let codes = $state<string[]>([]);
	let confirmPw = $state('');
	let sessions = $state<Schemas['Session'][]>([]);
	let audit = $state<Schemas['AuditEvent'][]>([]);

	async function changePassword() {
		try {
			await ok(api.POST('/me/password', { body: { current_password: curPw, new_password: newPw } }));
			curPw = newPw = newPw2 = '';
			app.toast('Password changed. Other sessions were signed out.');
			loadSecurity();
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}
	async function beginTotp() {
		try {
			totp = await ok(api.POST('/me/2fa/totp'));
			qr = await QRCode.toDataURL(totp.otpauth_uri, { margin: 1, width: 200 });
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}
	async function confirmTotp() {
		try {
			codes = (await ok(api.POST('/me/2fa/totp/confirm', { body: { code } }))).recovery_codes;
			totp = null;
			code = '';
			app.me = await ok(api.GET('/me'));
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}
	async function disableTotp() {
		try {
			await ok(api.DELETE('/me/2fa/totp', { body: { password: confirmPw, code } }));
			confirmPw = code = '';
			app.me = await ok(api.GET('/me'));
			app.toast('Two-factor authentication turned off');
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}
	async function regenCodes() {
		try {
			codes = (await ok(api.POST('/me/2fa/recovery-codes', { body: { password: confirmPw } }))).recovery_codes;
			confirmPw = '';
			app.me = await ok(api.GET('/me'));
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}
	async function loadSecurity() {
		sessions = (await ok(api.GET('/me/sessions'))).data;
		audit = (await ok(api.GET('/me/audit'))).data;
	}
	async function revoke(id: string) {
		await ok(api.DELETE('/me/sessions/{id}', { params: { path: { id } } }));
		loadSecurity();
	}
	async function revokeOthers() {
		await ok(api.DELETE('/me/sessions'));
		loadSecurity();
		app.toast('Signed out everywhere else');
	}

	// ---- tokens ----
	let tokens = $state<Schemas['APIToken'][]>([]);
	let tokenName = $state('');
	let tokenScopes = $state<Schemas['Scope'][]>(['tasks:read', 'tasks:write']);
	let newToken = $state('');
	const allScopes: Schemas['Scope'][] = ['tasks:read', 'tasks:write', 'notes:read', 'notes:write', 'export', 'account:read'];
	async function loadTokens() {
		tokens = (await ok(api.GET('/me/tokens'))).data;
	}
	async function createToken() {
		try {
			const t = await ok(api.POST('/me/tokens', { body: { name: tokenName, scopes: tokenScopes } }));
			newToken = t.token ?? '';
			tokenName = '';
			loadTokens();
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}
	async function deleteToken(id: string) {
		await ok(api.DELETE('/me/tokens/{id}', { params: { path: { id } } }));
		loadTokens();
	}

	// ---- admin ----
	let users = $state<Schemas['User'][]>([]);
	let invites = $state<Schemas['Invite'][]>([]);
	let inviteEmail = $state('');
	let inviteLink = $state('');
	async function loadAdmin() {
		users = (await ok(api.GET('/admin/users'))).data;
		invites = (await ok(api.GET('/admin/invites'))).data;
	}
	async function invite() {
		try {
			const inv = await ok(api.POST('/admin/invites', { body: inviteEmail ? { email: inviteEmail } : {} }));
			inviteLink = `${location.origin}/register?invite=${inv.token}`;
			inviteEmail = '';
			loadAdmin();
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}
	async function adminPatch(id: string, body: { disabled?: boolean; reset_2fa?: boolean }) {
		try {
			await ok(api.PATCH('/admin/users/{id}', { params: { path: { id } }, body }));
			loadAdmin();
		} catch (e) {
			app.toast(err(e), 'error');
		}
	}

	$effect(() => {
		if (tab === 'security') loadSecurity();
		if (tab === 'tokens') loadTokens();
		if (tab === 'admin') loadAdmin();
	});
</script>

<svelte:head><title>Settings · Blair</title></svelte:head>
<ViewHeader title="Settings" />

<div class="tabs" role="tablist">
	{#each tabs as [id, label] (id)}
		<button role="tab" aria-selected={tab === id} class:on={tab === id} onclick={() => (tab = id)}>{label}</button>
	{/each}
</div>

<div class="panel">
	{#if tab === 'profile'}
		<section>
			<h2>Profile</h2>
			<div class="field"><label class="label" for="s-name">Display name</label><input id="s-name" class="input" bind:value={name} /></div>
			<div class="field"><label class="label" for="s-email">Email</label><input id="s-email" class="input" value={app.me?.email} disabled /></div>
			<div class="field">
				<label class="label" for="s-tz">Time zone</label>
				<select id="s-tz" class="input" bind:value={tz}>{#each zones as z (z)}<option value={z}>{z}</option>{/each}</select>
			</div>
			<div class="field">
				<label class="label" for="s-ws">Week starts on</label>
				<select id="s-ws" class="input" bind:value={weekStart}><option value="monday">Monday</option><option value="sunday">Sunday</option></select>
			</div>
			<button class="btn btn-primary" onclick={saveProfile}>Save</button>
		</section>
		<section>
			<h2>Appearance</h2>
			<div class="seg" role="radiogroup" aria-label="Theme">
				{#each [['system', 'System'], ['light', 'Light'], ['dark', 'Dark']] as [v, l] (v)}
					<button role="radio" aria-checked={app.theme === v} class:on={app.theme === v} onclick={() => app.saveSettings({ theme: v })}>{l}</button>
				{/each}
			</div>
		</section>
		<section>
			<h2>Features</h2>
			<label class="toggle">
				<input type="checkbox" checked={app.planEnabled} onchange={(e) => app.saveSettings({ daily_plan: (e.currentTarget as HTMLInputElement).checked })} />
				<span>
					<b>Daily plan</b>
					<span class="muted">Pick what you'll work on each day, copy it for standup, and see tasks that keep slipping. Your plans are kept when this is off, and the API works either way.</span>
				</span>
			</label>
		</section>
		<section>
			<h2>Install the app</h2>
			<ul class="muted install">
				<li><b>iPhone / iPad:</b> Safari → Share → Add to Home Screen.</li>
				<li><b>Mac:</b> Safari → File → Add to Dock.</li>
				<li><b>Android:</b> Firefox or Chrome menu → Install / Add to Home screen.</li>
			</ul>
		</section>
		<section>
			<button class="btn" onclick={() => app.logout()}>Sign out</button>
		</section>
	{:else if tab === 'security'}
		{#if welcome && !app.me?.totp_enabled}
			<div class="callout">Welcome! Turn on two-factor authentication now. Blair is reachable from the internet.</div>
		{/if}
		<section>
			<h2>Two-factor authentication</h2>
			{#if codes.length}
				<p>Save these recovery codes somewhere safe. Each works once, and they won't be shown again.</p>
				<pre class="codes">{codes.join('\n')}</pre>
				<div class="row">
					<button class="btn" onclick={() => navigator.clipboard?.writeText(codes.join('\n')).then(() => app.toast('Copied'))}>Copy</button>
					<button class="btn btn-primary" onclick={() => (codes = [])}>I've saved them</button>
				</div>
			{:else if app.me?.totp_enabled}
				<p class="ok">✓ On · {app.me.recovery_codes_remaining} recovery codes left</p>
				<div class="field"><label class="label" for="s-cpw">Password</label><input id="s-cpw" class="input" type="password" autocomplete="current-password" bind:value={confirmPw} /></div>
				<div class="field"><label class="label" for="s-code">Current code (to turn off)</label><input id="s-code" class="input" inputmode="numeric" autocomplete="one-time-code" bind:value={code} /></div>
				<div class="row">
					<button class="btn" disabled={!confirmPw} onclick={regenCodes}>New recovery codes</button>
					{#if !app.status?.require_2fa}<button class="btn btn-ghost btn-danger" disabled={!confirmPw || !code} onclick={disableTotp}>Turn off 2FA</button>{/if}
				</div>
			{:else if totp}
				<ol class="steps">
					<li>Scan this with your authenticator app (1Password, Authy, Google Authenticator…).</li>
				</ol>
				<img src={qr} alt="QR code for your authenticator app" width="200" height="200" class="qr" />
				<p class="muted">Can't scan? Enter this key: <code class="secret">{totp.secret}</code></p>
				<div class="field"><label class="label" for="s-tc">Then enter the 6-digit code</label><input id="s-tc" class="input" inputmode="numeric" autocomplete="one-time-code" maxlength="7" bind:value={code} /></div>
				<button class="btn btn-primary" disabled={code.replace(/\s/g, '').length !== 6} onclick={confirmTotp}>Turn on</button>
			{:else}
				<p class="muted">Protect your account with a code from an authenticator app.</p>
				<button class="btn btn-primary" onclick={beginTotp}>Set up 2FA</button>
			{/if}
		</section>
		<section>
			<h2>Password</h2>
			<div class="field"><label class="label" for="s-cur">Current password</label><input id="s-cur" class="input" type="password" autocomplete="current-password" bind:value={curPw} /></div>
			<div class="field"><label class="label" for="s-new">New password (12+ characters)</label><input id="s-new" class="input" type="password" autocomplete="new-password" minlength="12" bind:value={newPw} /></div>
			<div class="field">
				<label class="label" for="s-new2">Confirm new password</label><input id="s-new2" class="input" type="password" autocomplete="new-password" bind:value={newPw2} aria-invalid={newPw2.length > 0 && newPw2 !== newPw} />
				{#if newPw2 && newPw2 !== newPw}<p class="muted" style="color: var(--danger)">Passwords don't match</p>{/if}
			</div>
			<button class="btn" disabled={!curPw || newPw.length < 12 || newPw !== newPw2} onclick={changePassword}>Change password</button>
		</section>
		<section>
			<h2>Sessions</h2>
			{#each sessions as s (s.id)}
				<div class="line">
					<div class="grow"><b>{s.user_agent.split(') ')[0].split(' (')[1] ?? s.user_agent.slice(0, 40)}</b>{#if s.current} <span class="badge">this device</span>{/if}<br /><span class="muted">{s.ip} · active {relativeTime(s.last_seen_at)}</span></div>
					{#if !s.current}<button class="btn btn-ghost" onclick={() => revoke(s.id)}>Sign out</button>{/if}
				</div>
			{/each}
			{#if sessions.length > 1}<button class="btn" onclick={revokeOthers}>Sign out everywhere else</button>{/if}
		</section>
		<section>
			<h2>Security log</h2>
			<div class="audit">
				{#each audit.slice(0, 30) as a (a.id)}
					<div class="line"><span class="grow">{a.kind.replace(/[._]/g, ' ')}</span><span class="muted">{a.ip} · {relativeTime(a.created_at)}</span></div>
				{/each}
			</div>
		</section>
	{:else if tab === 'tokens'}
		<section>
			<h2>Personal access tokens</h2>
			<p class="muted">For scripts and agents. Send as <code>Authorization: Bearer …</code>. See the <a href="/api/docs" target="_blank" rel="noopener">API docs</a>.</p>
			{#if newToken}
				<div class="callout">
					Copy this token now. It won't be shown again.
					<pre class="codes">{newToken}</pre>
					<div class="row"><button class="btn" onclick={() => navigator.clipboard?.writeText(newToken).then(() => app.toast('Copied'))}>Copy</button><button class="btn btn-ghost" onclick={() => (newToken = '')}>Done</button></div>
				</div>
			{/if}
			<div class="field"><label class="label" for="s-tn">Name</label><input id="s-tn" class="input" bind:value={tokenName} placeholder="e.g. migration agent" /></div>
			<fieldset class="scopes">
				<legend class="label">Scopes</legend>
				{#each allScopes as s (s)}
					<label><input type="checkbox" value={s} bind:group={tokenScopes} /> {s}</label>
				{/each}
			</fieldset>
			<button class="btn btn-primary" disabled={!tokenName.trim() || !tokenScopes.length} onclick={createToken}>Create token</button>
		</section>
		<section>
			{#each tokens as t (t.id)}
				<div class="line">
					<div class="grow"><b>{t.name}</b> <code>{t.prefix}…</code><br /><span class="muted">{t.scopes.join(', ')} · {t.last_used_at ? `used ${relativeTime(t.last_used_at)}` : 'never used'}</span></div>
					<button class="btn btn-ghost btn-danger" onclick={() => deleteToken(t.id)}>Revoke</button>
				</div>
			{:else}
				<p class="muted">No tokens.</p>
			{/each}
		</section>
	{:else if tab === 'data'}
		<section>
			<h2>Export</h2>
			<p class="muted">Everything, including completed and trashed items.</p>
			<div class="row">
				<a class="btn" href="/api/v1/export?format=json" download>Download JSON</a>
				<a class="btn" href="/api/v1/export?format=csv" download>Download CSV (tasks)</a>
			</div>
		</section>
		<section>
			<h2>Import</h2>
			<p class="muted">Blair has no built-in importers. Point an agent or script at the <a href="/api/docs" target="_blank" rel="noopener">API docs</a> ("Migrating into Blair"): batch upserts keep completion dates and are safe to re-run.</p>
		</section>
	{:else if tab === 'admin'}
		<section>
			<h2>Invite someone</h2>
			{#if app.status?.registration !== 'invite'}<p class="muted">Registration is <b>{app.status?.registration}</b>. Set <code>BLAIR_REGISTRATION=invite</code> for invite links to work.</p>{/if}
			<div class="row">
				<input class="input" type="email" bind:value={inviteEmail} placeholder="Email (optional, locks the invite)" aria-label="Invite email" />
				<button class="btn btn-primary" onclick={invite}>Create invite</button>
			</div>
			{#if inviteLink}<pre class="codes">{inviteLink}</pre>{/if}
			{#each invites as inv (inv.id)}
				<div class="line"><span class="grow">{inv.email ?? 'anyone'}</span><span class="muted">{inv.used_at ? 'used' : `expires ${new Date(inv.expires_at).toLocaleDateString()}`}</span></div>
			{/each}
		</section>
		<section>
			<h2>Users</h2>
			{#each users as u (u.id)}
				<div class="line">
					<div class="grow"><b>{u.display_name || u.email}</b> {#if u.is_admin}<span class="badge">admin</span>{/if}{#if u.disabled}<span class="badge">disabled</span>{/if}<br /><span class="muted">{u.email} · 2FA {u.totp_enabled ? 'on' : 'off'}</span></div>
					{#if u.id !== app.me?.id}
						{#if u.totp_enabled}<button class="btn btn-ghost" onclick={() => adminPatch(u.id, { reset_2fa: true })}>Reset 2FA</button>{/if}
						<button class="btn btn-ghost" onclick={() => adminPatch(u.id, { disabled: !u.disabled })}>{u.disabled ? 'Enable' : 'Disable'}</button>
					{/if}
				</div>
			{/each}
		</section>
	{/if}
</div>

<style>
	.tabs {
		display: flex;
		gap: 4px;
		flex-wrap: wrap;
		margin-bottom: 20px;
		border-bottom: 1px solid var(--border);
	}
	.tabs button {
		padding: 10px 14px;
		border: 0;
		background: none;
		color: var(--muted);
		border-bottom: 2px solid transparent;
		margin-bottom: -1px;
	}
	.tabs .on {
		color: var(--text);
		font-weight: 600;
		border-bottom-color: var(--accent);
	}
	.panel {
		max-width: 620px;
		display: flex;
		flex-direction: column;
		gap: 30px;
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 12px;
		align-items: flex-start;
	}
	section > :global(*) {
		max-width: 100%;
	}
	h2 {
		margin: 0;
		font-family: var(--font-display);
		font-weight: 600;
		font-size: 20px;
	}
	.field {
		width: 100%;
	}
	.row {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
		width: 100%;
	}
	.row .input {
		flex: 1;
		min-width: 200px;
	}
	.seg {
		display: flex;
		padding: 3px;
		gap: 2px;
		border-radius: var(--r);
		background: var(--chip);
	}
	.seg button {
		min-width: 80px;
		min-height: 34px;
		border: 0;
		border-radius: var(--r-sm);
		background: none;
		color: var(--muted);
	}
	.seg .on {
		background: var(--surface);
		color: var(--text);
		font-weight: 600;
	}
	.callout {
		width: 100%;
		padding: 12px 14px;
		border-radius: var(--r-sm);
		background: var(--accent-soft);
	}
	.codes {
		width: 100%;
		padding: 12px 14px;
		border-radius: var(--r-sm);
		background: var(--surface);
		border: 1px solid var(--border);
		font-family: var(--font-mono);
		font-size: 14px;
		white-space: pre-wrap;
		word-break: break-all;
	}
	.qr {
		border-radius: var(--r-sm);
		background: #fff;
		padding: 6px;
	}
	.secret {
		word-break: break-all;
	}
	.ok {
		color: var(--done);
		font-weight: 600;
		margin: 0;
	}
	.steps {
		margin: 0;
		padding-left: 18px;
	}
	.line {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		padding: 10px 0;
		border-bottom: 1px solid var(--border);
	}
	.grow {
		flex: 1;
		min-width: 0;
	}
	.audit {
		width: 100%;
		max-height: 360px;
		overflow-y: auto;
	}
	.badge {
		font-size: 11px;
		padding: 1px 6px;
		border-radius: 4px;
		background: var(--chip);
		color: var(--muted);
		font-weight: 500;
	}
	.scopes {
		border: 0;
		padding: 0;
		margin: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 6px;
		width: 100%;
	}
	.scopes label {
		display: flex;
		gap: 6px;
		align-items: center;
		font-size: 13px;
	}
	.install {
		margin: 0;
		padding-left: 18px;
	}
	.muted {
		color: var(--muted);
		font-size: 13px;
		margin: 0;
	}
	.toggle {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		font-size: 14px;
	}
	.toggle > span {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.toggle input {
		margin-top: 3px;
	}
</style>
