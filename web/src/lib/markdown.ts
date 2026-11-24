import DOMPurify from 'dompurify';
import MarkdownIt from 'markdown-it';

// SPEC §6: markdown is rendered client-side (html disabled) and sanitized.
const block = new MarkdownIt({ html: false, linkify: true, breaks: true });
const inline = new MarkdownIt('zero', { html: false, linkify: true }).enable([
	'emphasis',
	'strikethrough',
	'backticks',
	'link',
	'linkify',
	'escape',
	'entity'
]);

let hooked = false;
function hook() {
	if (hooked || typeof window === 'undefined') return;
	hooked = true;
	DOMPurify.addHook('afterSanitizeAttributes', (node) => {
		if (node.tagName === 'A') {
			node.setAttribute('target', '_blank');
			node.setAttribute('rel', 'noopener noreferrer nofollow');
		}
	});
}

const SAFE_URI = /^(?:https?:|mailto:)/i;

/** Title-safe inline markdown: bold, italic, code, strike, links. */
export function renderInline(src: string): string {
	hook();
	return DOMPurify.sanitize(inline.renderInline(src), {
		ALLOWED_TAGS: ['strong', 'em', 'del', 's', 'code', 'a'],
		ALLOWED_ATTR: ['href'],
		ALLOWED_URI_REGEXP: SAFE_URI
	});
}

/** Full GFM-ish markdown for descriptions and notes, with task-list glyphs. */
export function renderBlock(src: string): string {
	hook();
	const html = block
		.render(src)
		.replace(/<li>\[ \]\s?/g, '<li class="task-item"><span class="tick" aria-label="not done">☐</span> ')
		.replace(/<li>\[x\]\s?/gi, '<li class="task-item done"><span class="tick" aria-label="done">☑</span> ');
	return DOMPurify.sanitize(html, {
		ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'del', 's', 'code', 'pre', 'a', 'ul', 'ol', 'li', 'blockquote', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'span'],
		ALLOWED_ATTR: ['href', 'class', 'aria-label'],
		ALLOWED_URI_REGEXP: SAFE_URI
	});
}

/** Plain text for places that can't show markup (document title, aria labels). */
export function plain(src: string): string {
	return src.replace(/[*_~`]/g, '').replace(/\[([^\]]+)\]\([^)]+\)/g, '$1');
}
