export type RecordStatus = 'accepted' | 'rejected' | 'superseded';

export type RecordIndexRow = {
	number: string;
	href: string;
	date: string;
	status: RecordStatus;
	title: string;
};
