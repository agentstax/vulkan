export type PostHeader =
	| { kind: 'posted'; postedDate: string; reportHref: string }
	| { kind: 'accepted'; postedDate: string };
