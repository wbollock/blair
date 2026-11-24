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
	const invite = page.url.searchParams.get('invite') ?? '';

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			await ok(api.POST('/auth/register', { body: { invite_token: invite, email, password, display_name: name, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone } }));
			await app.loadSession();
			await goto('/settings?tab=security&welcome=1');
		} catch (err) {
			error = err instanceof ApiError ? err.field('password') || err.field('email') || err.message : 'Registration failed';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Join · Blair</title></svelte:head>

<AuthCard title="Create your account">
	<form onsubmit={submit}>
		{#if error}<div class="error" role="alert">{error}</div>{/if}
		<div><label class="label" for="name">Your name</label><input id="name" class="input" autocomplete="name" bind:value={name} /></div>
		<div><label class="label" for="email">Email</label><input id="email" class="input" type="email" autocomplete="username" required bind:value={email} /></div>
		<div><label class="label" for="pw">Password (12+ characters)</label><input id="pw" class="input" type="password" autocomplete="new-password" minlength="12" required bind:value={password} /></div>
		<button class="btn btn-primary" disabled={busy}>Create account</button>
		<a href="/login">I already have an account</a>
	</form>
</AuthCard>
