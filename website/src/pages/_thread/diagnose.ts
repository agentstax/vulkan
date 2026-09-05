import type { DiagnoseQuery } from '../../components/diagnose-queries/types';
import { codeRecords } from '../../data/codes';

// diagnoseQueries returns null for a code that declares none -- most
// conditions have nothing to look at, and no section renders for them.
export function diagnoseQueries(code: string): DiagnoseQuery[] | null {
	const record = codeRecords[code];
	if (record === undefined || record.kind === 'metric' || record.kind === 'alert') {
		return null;
	}
	return record.queries ?? null;
}

// fixPlaceholders is the names a code's fix substitutes. Most fixes name no
// value the caller supplies, so the common answer is an empty list.
export function fixPlaceholders(code: string): string[] {
	const record = codeRecords[code];
	if (record === undefined || record.kind !== 'error') {
		return [];
	}
	return record.fix_placeholders ?? [];
}

// pastePlaceholders is every name the thread can fill from one pasted line --
// the queries' and the fix's together. The paste box reports against it.
export function pastePlaceholders(code: string): string[] {
	const queries = diagnoseQueries(code) ?? [];
	return [
		...new Set([...queries.flatMap((query) => query.placeholders), ...fixPlaceholders(code)]),
	];
}
