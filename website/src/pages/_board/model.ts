export type BoardRowData = {
	title: string;
	href: string;
	description: string;
	threadCount: number;
	lastPostTitle: string;
	lastPostHref: string;
	lastPostDate: string;
	scopeHrefs: string[];
};

export type StickyRowData = {
	title: string;
	href: string;
	lastUpdatedDate: string;
};

export type SiteStats = {
	threadCount: number;
	codeCount: number;
	decisionRecordCount: number;
};
