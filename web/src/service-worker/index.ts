// Caches the app shell so the installed app opens without a connection (SPEC §12.2).
// API calls always go to the network; offline task capture lives in the app (§12.3).
import { self } from '$app/service-worker';
import { assets, immutable } from '$app/manifest';

const SHELL = '/';
// Static files minus the API docs bundle, which only /api/docs uses.
const abs = (p: string) => '/' + p.replace(/^\/+/, '');
const PRECACHE = [SHELL, ...immutable.map((f) => abs(f.path)), ...assets.map((f) => abs(f.path)).filter((p) => !p.startsWith('/vendor/'))];
// Hashed build file names change with every build, so they name the cache.
let h = 0;
for (const c of immutable.map((f) => f.path).join()) h = (h * 31 + c.charCodeAt(0)) | 0;
const CACHE = `blair-${(h >>> 0).toString(36)}`;

self.addEventListener('install', (event) => {
	event.waitUntil(
		caches
			.open(CACHE)
			.then((c) => c.addAll(PRECACHE))
			.then(() => self.skipWaiting())
	);
});

self.addEventListener('activate', (event) => {
	event.waitUntil(
		caches
			.keys()
			.then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
			.then(() => self.clients.claim())
	);
});

self.addEventListener('fetch', (event) => {
	const req = event.request;
	const url = new URL(req.url);
	if (req.method !== 'GET' || url.origin !== location.origin || url.pathname.startsWith('/api/')) return;

	// Pages: network first so a deploy shows up right away; the cached shell when offline.
	if (req.mode === 'navigate') {
		event.respondWith(fetch(req).catch(async () => (await caches.match(SHELL)) ?? Response.error()));
		return;
	}
	// Build output and static files: cache first, they are versioned with the cache.
	if (PRECACHE.includes(url.pathname)) {
		event.respondWith(caches.match(req).then((hit) => hit ?? fetch(req)));
	}
});
