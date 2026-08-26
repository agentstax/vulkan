import type { DiagnoseQuery } from '../../components/diagnose-queries/types';
import { codeRecords } from '../../data/codes';

// diagnoseQueries returns null for a code that declares none -- most
// conditions have nothing to look at, and no section renders for them.
export function diagnoseQueries(code: string): DiagnoseQuery[] | null {
	const record = codeRecords[code];
	if (record === undefined || record.kind === 'metric') {
		return null;
	}
	return record.queries ?? null;
}
