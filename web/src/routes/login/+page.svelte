<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, ok } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import AuthCard from '#lib/components/AuthCard.svelte';

	let email = $state('');
	let password = $state('');
	let code = $state('');
	let useRecovery = $state(false);
	let step = $state<'password' | 'mfa'>(app.status?.mfa_pending ? 'mfa' : 'password');
	let error = $state('');
	let busy = $state(false);

	function next() {
		const n = page.url.searchParams.get('next');
		return n && n.startsWith('/') && !n.startsWith('//') ? n : '/today';
	}
	async function finish() {
		await app.loadSession();
		await goto(next());
	}
	async function login(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const r = await ok(api.POST('/auth/login', { body: { email, password } }));
			if (r.mfa_required) {
				step = 'mfa';
				password = '';
			} else await finish();
		} catch (err) {
			error = err instanceof ApiError && err.status === 429 ? err.message : 'Wrong email or password.';
		} finally {
			busy = false;
		}
	}
	async function verify(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			await ok(api.POST('/auth/login/mfa', { body: useRecovery ? { recovery_code: code } : { code } }));
			await finish();
		} catch (err) {
			if (err instanceof ApiError && err.problem.type?.endsWith('mfa_expired')) {
				step = 'password';
				error = 'That took too long. Sign in again.';
			} else error = err instanceof ApiError && err.status === 429 ? err.message : "That code didn't work.";
			code = '';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Sign in · Blair</title></svelte:head>

{#if step === 'password'}
	<AuthCard title="Sign in">
		<form onsubmit={login}>
			{#if error}<div class="error" role="alert">{error}</div>{/if}
			<div><label class="label" for="email">Email</label><input id="email" class="input" type="email" autocomplete="username" required bind:value={email} /></div>
			<div><label class="label" for="pw">Password</label><input id="pw" class="input" type="password" autocomplete="current-password" required bind:value={password} /></div>
			<button class="btn btn-primary" disabled={busy}>Sign in</button>
		</form>
	</AuthCard>
{:else}
	<AuthCard title="Two-factor check" subtitle={useRecovery ? 'Enter one of your recovery codes.' : 'Enter the 6-digit code from your authenticator app.'}>
		<form onsubmit={verify}>
			{#if error}<div class="error" role="alert">{error}</div>{/if}
			{#if useRecovery}
				<!-- svelte-ignore a11y_autofocus -->
				<input class="input" autocomplete="one-time-code" placeholder="xxxx-xxxx-xxxx" required bind:value={code} autofocus aria-label="Recovery code" />
			{:else}
				<!-- svelte-ignore a11y_autofocus -->
				<input class="input code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9 ]*" maxlength="7" required bind:value={code} autofocus aria-label="Authentication code" />
			{/if}
			<button class="btn btn-primary" disabled={busy}>Verify</button>
			<button type="button" class="btn btn-ghost" onclick={() => { useRecovery = !useRecovery; code = ''; }}>{useRecovery ? 'Use authenticator code' : 'Use a recovery code'}</button>
		</form>
	</AuthCard>
{/if}

<style>
	.code {
		font-family: var(--font-mono);
		font-size: 22px;
		letter-spacing: 0.3em;
		text-align: center;
	}
</style>
