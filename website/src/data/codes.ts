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
	fix_placeholders?: string[];
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
	scope: 'system' | 'topic' | 'consumer_group' | 'consumer_session';
	attribute_keys?: string[];
};

// An alert declares no diagnose queries either: the alert message on
// `__system.alerts` carries its own detail and hint.
export type AlertRecord = {
	code: string;
	kind: 'alert';
	name: string;
	description: string;
	scope: 'system' | 'topic' | 'consumer_group';
	severity: string;
};

export type CodeRecord = ErrorRecord | EventRecord | MetricRecord | AlertRecord;

// The JSON's own type says kind is a string. The four kinds are fixed on the
// Go side, and codes.test.ts walks the export to prove no fifth one arrived.
export const codeRecords: Record<string, CodeRecord> = codes.codes as Record<string, CodeRecord>;
