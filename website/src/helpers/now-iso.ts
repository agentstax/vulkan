// lives outside the .svelte.ts module so the Date never looks like state
export function nowIso(): string {
	return new Date().toISOString();
}
