<script lang="ts">
	import type { Snippet } from 'svelte';
	let { title, count, tone = '', children }: { title: string; count?: number; tone?: string; children: Snippet } = $props();
</script>

<section class="sec">
	<h2 class="eyebrow" style={tone ? `color: ${tone}` : ''}>{title}{count !== undefined ? ` · ${count}` : ''}</h2>
	{@render children()}
</section>

<style>
	.sec {
		margin-top: 22px;
	}
	h2 {
		margin: 0 0 4px 8px;
	}
</style>
