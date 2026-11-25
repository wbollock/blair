// Formats a day's plan for pasting into Slack or a standup doc: plain text
// with markdown links, plus HTML so rich-text editors render the links.

const MD_LINK = /\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/;

/** First URL in a description: a markdown link, an <a href>, or a bare URL. */
export function firstUrl(description: string): string {
	const found: { at: number; url: string }[] = [];
	const md = MD_LINK.exec(description);
	if (md) found.push({ at: md.index, url: md[2] });
	const a = /<a\b[^>]*\bhref\s*=\s*["'](https?:\/\/[^"']+)["']/i.exec(description);
	if (a) found.push({ at: a.index, url: a[1].replace(/&amp;/g, '&') });
	const bare = /https?:\/\/[^\s<>)"']+/.exec(description);
	if (bare) found.push({ at: bare.index, url: bare[0] });
	found.sort((x, y) => x.at - y.at);
	return found[0]?.url ?? '';
}

/** The title as a markdown link to the first URL in its notes, unless it already has a link. */
export function linkedTitle(title: string, description: string): string {
	if (MD_LINK.test(title)) return title;
	const url = firstUrl(description);
	return url ? `[${title}](${url})` : title;
}

const escapeHTML = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

export function standup(tasks: { title: string; description: string }[], heading = 'Tasks'): { plain: string; html: string } {
	const lines = tasks.map((t) => linkedTitle(t.title, t.description));
	const plain = [`*${heading}*`, ...lines.map((l) => `• ${l}`)].join('\n');
	const toHTML = (l: string) =>
		escapeHTML(l).replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, (_, text: string, url: string) => `<a href="${url}">${text}</a>`);
	const html = `<b>${escapeHTML(heading)}</b><br>\n` + lines.map((l) => `• ${toHTML(l)}`).join('<br>\n');
	return { plain, html };
}

/** Copies both forms; falls back to plain text where ClipboardItem is missing. */
export async function copyRich(text: { plain: string; html: string }) {
	if (typeof ClipboardItem !== 'undefined' && navigator.clipboard?.write) {
		try {
			await navigator.clipboard.write([
				new ClipboardItem({ 'text/plain': new Blob([text.plain], { type: 'text/plain' }), 'text/html': new Blob([text.html], { type: 'text/html' }) })
			]);
			return;
		} catch {
			/* fall through */
		}
	}
	await navigator.clipboard.writeText(text.plain);
}
