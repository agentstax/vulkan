import codes from './codes.json';

// QueryRecord is one declared diagnose query. The placeholders come from the
// library's own parse of the SQL, so no reader here re-derives them.
export type QueryRecord = {
	label: string;
	sql: string;
	placeholders: string[];
};

// A declaration's kind decides which parts it has, so the record is a union on
// kind rather than one shape of optional fields. Optional here means what the
// declaring constructor leaves optional: NewError guards problem and recovery
// but not fix, NewMetric guards name, kind and description but not unit, and
// Diagnose is a separate call on the Error or Event.
export type ErrorRecord = {
	code: string;
	kind: 'error';
	problem: string;
	recovery: 'transient' | 'permanent';
	fix?: string;
	queries?: QueryRecord[];
};

export type EventRecord = {
	code: string;
	kind: 'event';
	message: string;
	queries?: QueryRecord[];
};

// A metric declares no diagnose queries -- the exclusion is deliberate, so the
// record has nowhere to put them.
export type MetricRecord = {
	code: string;
	kind: 'metric';
	name: string;
	metric_kind: string;
	unit?: string;
	description: string;
};

export type CodeRecord = ErrorRecord | EventRecord | MetricRecord;

// The JSON's own type says kind is a string. The three kinds are fixed on the
// Go side, and codes.test.ts walks the export to prove no fourth one arrived.
export const codeRecords: Record<string, CodeRecord> = codes.codes as Record<string, CodeRecord>;
