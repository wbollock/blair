<script lang="ts">
	import { DropdownMenu } from 'bits-ui';
	import type { Snippet } from 'svelte';

	let { set = false, label, trigger, items, ariaLabel }: { set?: boolean; label?: string; trigger?: Snippet; items: Snippet; ariaLabel?: string } = $props();
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger class="chip {set ? 'set' : ''}" aria-label={ariaLabel}>
		{#if trigger}{@render trigger()}{:else}{label}{/if}
	</DropdownMenu.Trigger>
	<DropdownMenu.Portal>
		<DropdownMenu.Content class="menu" align="start" sideOffset={6} collisionPadding={8}>
			{@render items()}
		</DropdownMenu.Content>
	</DropdownMenu.Portal>
</DropdownMenu.Root>
