<script lang="ts" module>
	let focusTarget: HTMLInputElement | null = null;
	/** Focus the quick-add input from anywhere (q / n shortcuts). */
	export function focusQuickAdd() {
		focusTarget?.focus();
	}
</script>

<script lang="ts">
	import Plus from '@lucide/svelte/icons/plus';
	import { app } from '#lib/state.svelte.ts';

	let { autofocus = false, compact = false, oncreated }: { autofocus?: boolean; compact?: boolean; oncreated?: () => void } = $props();
	let text = $state('');
	let input: HTMLInputElement;
	let scrollX = $state(0);
	let caret = $state(0);
	let focused = $state(false);
	let active = $state(0);

	const parsed = $derived(app.parse(text, app.view?.projectId));
	const segments = $derived.by(() => {
		const out: { text: string; kind?: string; isNew?: boolean }[] = [];
		let at = 0;
		for (const t of parsed.tokens) {
			if (t.start > at) out.push({ text: text.slice(at, t.start) });
			const isNew = (t.kind === 'project' && t.value === parsed.projectNew) || (t.kind === 'label' && parsed.labelsNew.includes(t.value));
			out.push({ text: t.text, kind: t.kind, isNew });
			at = t.end;
		}
		if (at < text.length) out.push({ text: text.slice(at) });
		return out;
	});

	// Autocomplete for the word under the caret.
	const word = $derived.by(() => {
		const before = text.slice(0, caret);
		const start = Math.max(before.lastIndexOf(' '), before.lastIndexOf('\t')) + 1;
		const w = before.slice(start);
		return { start, w };
	});
	const suggestions = $derived.by(() => {
		const { w } = word;
		if (!focused || w.length < 1) return [];
		const q = w.slice(1).toLowerCase().replace(/^"/, '');
		const sigil = w[0];
		let names: string[] = [];
		if (sigil === '#') names = app.projects.filter((p) => !p.archived_at).map((p) => p.name);
		else if (sigil === '@') names = app.labels.map((l) => l.name);
		else if (sigil === '/') {
			const proj = app.projects.find((p) => p.name === parsed.project) ?? app.project(app.view?.projectId);
			names = proj?.columns.map((c) => c.name) ?? [];
		} else return [];
		return names
			.filter((n) => n.toLowerCase().replace(/\s+/g, '').startsWith(q.replace(/\s+/g, '')) && n.toLowerCase() !== q)
			.slice(0, 6)
			.map((n) => ({ sigil, name: n }));
	});
	$effect(() => {
		if (active >= suggestions.length) active = 0;
	});

	function accept(i: number) {
		const s = suggestions[i];
		if (!s) return;
		const token = s.sigil + (s.sigil !== '@' && /\s/.test(s.name) ? `"${s.name}"` : s.name.replace(/\s+/g, '-'));
		const rest = text.slice(caret).replace(/^\S*/, '');
		text = text.slice(0, word.start) + token + ' ' + rest.trimStart();
		const pos = word.start + token.length + 1;
		requestAnimationFrame(() => {
			input.setSelectionRange(pos, pos);
			caret = pos;
		});
	}

	async function submit(open: boolean) {
		const value = text.trim();
		if (!value) return;
		text = '';
		caret = 0;
		await app.createFromText(value, { open });
		oncreated?.();
		input?.focus();
	}

	/** Pasting several lines adds one task per line (list bullets and checkboxes are stripped). */
	async function onpaste(e: ClipboardEvent) {
		const lines = (e.clipboardData?.getData('text') ?? '')
			.split(/\r?\n/)
			.map((l) => l.replace(/^\s*(?:[-*+•]\s+(?:\[[ xX]\]\s+)?|\d+[.)]\s+)/, '').trim())
			.filter(Boolean);
		if (lines.length < 2) return;
		e.preventDefault();
		if (lines.length > 25 && !confirm(`Add ${lines.length} tasks?`)) return;
		for (const line of lines) await app.createFromText(line);
		app.toast(`Added ${lines.length} tasks`);
		input?.focus();
	}

	function onkeydown(e: KeyboardEvent) {
		if (suggestions.length) {
			if (e.key === 'ArrowDown') {
				active = (active + 1) % suggestions.length;
				e.preventDefault();
				return;
			}
			if (e.key === 'ArrowUp') {
				active = (active - 1 + suggestions.length) % suggestions.length;
				e.preventDefault();
				return;
			}
			if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
				accept(active);
				e.preventDefault();
				return;
			}
		}
		if (e.key === 'Enter') {
			e.preventDefault();
			submit(e.shiftKey);
		} else if (e.key === 'Escape') {
			if (text) text = '';
			else input.blur();
		}
	}
	const sync = () => {
		caret = input.selectionStart ?? text.length;
		scrollX = input.scrollLeft;
	};
	$effect(() => {
		focusTarget = input;
		if (autofocus) input.focus();
		return () => {
			if (focusTarget === input) focusTarget = null;
		};
	});
</script>

<div class="qa" class:compact class:focused>
	<form
		class="box"
		onsubmit={(e) => {
			e.preventDefault();
			submit(false);
		}}
	>
		<Plus size={18} class="plus" aria-hidden="true" />
		<div class="field">
			<div class="mirror" aria-hidden="true" style="transform: translateX({-scrollX}px)">
				{#each segments as s, i (i)}{#if s.kind}<mark class="tok k-{s.kind}" class:new={s.isNew}>{s.text}</mark>{:else}{s.text}{/if}{/each}
			</div>
			<input
				bind:this={input}
				bind:value={text}
				{onkeydown}
				{onpaste}
				oninput={sync}
				onkeyup={sync}
				onclick={sync}
				onscroll={sync}
				onfocus={() => (focused = true)}
				onblur={() => setTimeout(() => (focused = false), 120)}
				placeholder={app.view?.name === 'Today' ? 'Add a task for today…' : 'Add a task…  try “Fix gutter #homelab !high fri 9am”'}
				aria-label="New task"
				autocomplete="off"
				autocapitalize="sentences"
				enterkeyhint="done"
				spellcheck="false"
				maxlength="2000"
			/>
		</div>
		<button type="submit" class="add" aria-label="Add task" disabled={!text.trim()}>Add</button>
	</form>
	{#if suggestions.length}
		<ul class="menu suggest" role="listbox" aria-label="Suggestions">
			{#each suggestions as s, i (s.sigil + s.name)}
				<li role="option" aria-selected={i === active}>
					<button type="button" class="menu-item" class:hl={i === active} onmousedown={(e) => { e.preventDefault(); accept(i); }}>{s.sigil}{s.name}</button>
				</li>
			{/each}
		</ul>
	{:else if focused && !compact}
		<div class="hint">
			<span><b>#</b>project</span><span><b>/</b>column</span><span><b>@</b>label</span><span><b>!</b>1–4</span><span>“fri 9am”</span><span>“every mon”</span>
			<span class="sp"></span><span>↵ add</span><span>⇧↵ add &amp; open</span>
		</div>
	{/if}
</div>

<style>
	.qa {
		position: relative;
	}
	.box {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 50px;
		padding: 0 6px 0 14px;
		border-radius: var(--r);
		background: var(--surface);
		border: 1.5px solid var(--border-strong);
		transition: border-color var(--dur) var(--ease), box-shadow var(--dur) var(--ease);
	}
	.focused .box {
		border-color: var(--accent);
		box-shadow: var(--focus-ring);
	}
	.box :global(.plus) {
		color: var(--accent);
		flex-shrink: 0;
	}
	.field {
		position: relative;
		flex: 1;
		min-width: 0;
		height: 100%;
		overflow: hidden;
	}
	.mirror,
	input {
		font-size: 15px;
		font-family: var(--font-ui);
		letter-spacing: normal;
		white-space: pre;
		line-height: 46px;
	}
	.mirror {
		position: absolute;
		inset: 0;
		pointer-events: none;
		color: var(--text);
	}
	input {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		padding: 0;
		border: 0;
		outline: none;
		background: transparent;
		color: transparent;
		caret-color: var(--text);
	}
	input::placeholder {
		color: var(--faint);
	}
	input::selection {
		background: var(--accent-soft);
		color: var(--text);
	}
	.tok {
		color: inherit;
		border-radius: 4px;
		background: var(--chip);
		box-shadow: 0 0 0 2px var(--chip);
	}
	.tok.k-project {
		background: var(--accent-soft);
		box-shadow: 0 0 0 2px var(--accent-soft);
	}
	.tok.k-priority {
		background: var(--p3-soft);
		box-shadow: 0 0 0 2px var(--p3-soft);
	}
	.tok.new {
		outline: 1px dashed var(--accent);
	}
	.add {
		height: 36px;
		padding: 0 14px;
		border-radius: var(--r-sm);
		border: 0;
		background: var(--accent);
		color: var(--on-accent);
		font-weight: 600;
	}
	.add:disabled {
		background: var(--chip);
		color: var(--faint);
		cursor: default;
	}
	.hint {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		padding: 8px 4px 0;
		font-size: 12px;
		color: var(--muted);
	}
	.hint b {
		color: var(--accent-ink);
	}
	.hint .sp {
		flex: 1;
	}
	.suggest {
		position: absolute;
		left: 36px;
		top: 54px;
		margin: 0;
		list-style: none;
		z-index: 30;
	}
	.suggest .hl {
		background: var(--selected);
	}
	.compact .box {
		height: 46px;
	}
	@media (max-width: 640px) {
		.hint .sp,
		.hint span:nth-last-child(-n + 2) {
			display: none;
		}
	}
</style>
