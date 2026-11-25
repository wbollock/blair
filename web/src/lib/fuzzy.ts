/** Small fuzzy matcher for the command palette: characters must appear in order;
 * consecutive runs, word starts and early matches score higher. */
export interface FuzzyMatch {
	score: number;
	/** Indices of matched characters in the target, for highlighting. */
	hits: number[];
}

const isWordStart = (s: string, i: number) => i === 0 || /[\s\-_/#@·.:]/.test(s[i - 1]) || (s[i - 1] === s[i - 1].toLowerCase() && s[i] !== s[i].toLowerCase());

export function fuzzy(query: string, target: string): FuzzyMatch | null {
	const q = query.toLowerCase().replace(/\s+/g, '');
	if (!q) return { score: 0, hits: [] };
	const t = target.toLowerCase();
	// Contiguous substring matches beat scattered ones.
	const sub = t.indexOf(q);
	if (sub >= 0) {
		const hits = Array.from({ length: q.length }, (_, k) => sub + k);
		return { score: 100 + (isWordStart(target, sub) ? 40 : 0) - sub - target.length / 10, hits };
	}
	const hits: number[] = [];
	let score = 0;
	let ti = 0;
	for (const ch of q) {
		const found = t.indexOf(ch, ti);
		if (found < 0) return null;
		score += found === hits[hits.length - 1] + 1 ? 8 : isWordStart(target, found) ? 6 : 1;
		score -= Math.min(found - ti, 6) * 0.5;
		hits.push(found);
		ti = found + 1;
	}
	return { score: score - target.length / 20, hits };
}

/** Ranks items by fuzzy score against their label (and optional extra search text). */
export function rank<T>(query: string, items: T[], text: (t: T) => string, limit = 30): { item: T; hits: number[] }[] {
	const out: { item: T; hits: number[]; score: number }[] = [];
	for (const item of items) {
		const m = fuzzy(query, text(item));
		if (m) out.push({ item, hits: m.hits, score: m.score });
	}
	return out.sort((a, b) => b.score - a.score).slice(0, limit);
}
