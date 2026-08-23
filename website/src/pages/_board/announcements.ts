export type Announcement = {
	title: string;
	href: string;
	date: string; // the milestone's HISTORY.md entry date
};

export const announcements: Announcement[] = [
	{
		title: 'Migration compat gate ships — old binaries stay safe through additive releases',
		href: '/guides/migrations/',
		date: '2026-08-22',
	},
	{
		title: 'Per-topic table families — every topic gets its own message_log',
		href: '/concepts/architecture/',
		date: '2026-08-22',
	},
	{
		title: 'Every error and log event gets a VK code and its own page',
		href: '/errors/',
		date: '2026-08-20',
	},
];
