// The shape tools/compatexport writes. Field names are the export's own
// snake_case keys, so this file is the contract between the Go writer and
// every reader on the site.

export type SchemaSupport = 'supported' | 'older_than_build' | 'newer_than_build';

export type CellExport = {
	build_version: number;
	support: SchemaSupport;
};

export type RowExport = {
	version: number;
	min_compatible_version: number;
	cells: CellExport[];
};

export type StepExport = {
	version: number;
	min_compatible_version: number;
};

export type ScopeExport = {
	version: number;
	steps: StepExport[];
	rows: RowExport[];
};

export type Export = {
	system: ScopeExport;
	topic: ScopeExport;
};
