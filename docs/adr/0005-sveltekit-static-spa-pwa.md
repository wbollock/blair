# ADR-0005: SvelteKit static SPA as an installable PWA

Status: Accepted · Date: 2026-11-24

## Context
Needs a responsive, polished UI on desktop and phone, installable on iOS/macOS
Safari and Android Firefox/Chrome, with offline quick-add and touch-friendly
kanban drag-and-drop. Server-rendered htmx was considered but is weak for
offline capture and rich DnD.

## Decision
- SvelteKit 2 + Svelte 5 runes, TypeScript strict, `adapter-static` with SPA
  fallback; all data via the public API (no private endpoints).
- Tailwind CSS v4 + CSS-variable design tokens; Bits UI headless primitives;
  Lucide icons; `svelte-dnd-action` for DnD (touch + keyboard).
- Markdown rendered client-side: markdown-it (html off) + DOMPurify.
- SvelteKit service worker for app-shell precache; `idb` for the offline
  outbox and read cache (see ADR-0008).
- CSP hashes via `kit.csp` so no `unsafe-inline` scripts are needed.

## Consequences
- Two toolchains (Go + Node) in dev and CI; mise pins both.
- Small bundles keep the mobile performance budget achievable.
