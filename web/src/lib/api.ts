import createClient, { type Middleware } from 'openapi-fetch';
import type { components, paths } from './api/schema';

export type Schemas = components['schemas'];
export type Task = Schemas['Task'];
export type Project = Schemas['Project'];
export type Column = Schemas['Column'];
export type Label = Schemas['Label'];
export type Note = Schemas['Note'];
export type User = Schemas['User'];
export type Me = Schemas['Me'];
export type ChecklistItem = Schemas['ChecklistItem'];
export type Status = Schemas['Status'];
export type Problem = Schemas['Problem'];
export type AuthStatus = Schemas['AuthStatus'];
export type Notification = Schemas['Notification'];

export class ApiError extends Error {
	constructor(
		readonly status: number,
		readonly problem: Partial<Problem>
	) {
		super(problem.detail || problem.title || `Request failed (${status})`);
	}
	field(name: string): string | undefined {
		return this.problem.errors?.find((e) => e.field === name)?.message;
	}
}

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
	onUnauthorized = fn;
}

const auth: Middleware = {
	onResponse({ response, request }) {
		if (response.status === 401 && !new URL(request.url).pathname.startsWith('/api/v1/auth/')) onUnauthorized?.();
		return response;
	}
};

/** Typed API client. Cookie auth; the custom header satisfies the CSRF check. */
export const api = createClient<paths>({
	baseUrl: '/api/v1',
	credentials: 'same-origin',
	headers: { 'X-Requested-With': 'blair' }
});
api.use(auth);

/** Unwraps an openapi-fetch result or throws ApiError. */
export async function ok<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
	const { data, error, response } = await p;
	if (!response.ok) throw new ApiError(response.status, (error ?? {}) as Partial<Problem>);
	return data as T;
}

/** RFC 9562 UUIDv7: time-ordered ids generated on the client (ADR-0008). */
export function uuidv7(): string {
	const b = new Uint8Array(16);
	crypto.getRandomValues(b);
	const ts = BigInt(Date.now());
	for (let i = 0; i < 6; i++) b[i] = Number((ts >> BigInt(8 * (5 - i))) & 0xffn);
	b[6] = (b[6] & 0x0f) | 0x70;
	b[8] = (b[8] & 0x3f) | 0x80;
	const h = [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
	return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}
