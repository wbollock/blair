<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, ok } from '#lib/api.ts';
	import { app } from '#lib/state.svelte.ts';
	import AuthCard from '#lib/components/AuthCard.svelte';

	let email = $state('');
	let name = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	const token = page.url.searchParams.get('token') ?? '';

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			await ok(api.POST('/auth/setup', { body: { token, email, password, display_name: name, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone } }));
			await app.loadSession();
			await goto('/settings?tab=security&welcome=1');
		} catch (err) {
			error = err instanceof ApiError ? err.field('password') || err.field('email') || err.message : 'Setup failed';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Set up · Blair</title></svelte:head>

<AuthCard title="Create the admin account" subtitle="This link works once. Next you'll turn on two-factor authentication.">
	{#if !token}
		<div class="error">Open the setup link printed in the server log (it contains a one-time token).</div>
	{:else}
		<form onsubmit={submit}>
			{#if error}<div class="error" role="alert">{error}</div>{/if}
			<div><label class="label" for="name">Your name</label><input id="name" class="input" autocomplete="name" bind:value={name} /></div>
			<div><label class="label" for="email">Email</label><input id="email" class="input" type="email" autocomplete="username" required bind:value={email} /></div>
			<div><label class="label" for="pw">Password <span class="hint">(12+ characters)</span></label><input id="pw" class="input" type="password" autocomplete="new-password" minlength="12" required bind:value={password} /></div>
			<button class="btn btn-primary" disabled={busy}>Create account</button>
		</form>
	{/if}
</AuthCard>

<style>
	.hint {
		font-weight: 400;
	}
</style>
