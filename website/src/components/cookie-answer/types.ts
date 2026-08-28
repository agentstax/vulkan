// What one answer says. The privacy statement is not here -- it is one
// fact the component renders under every answer, not a per-answer field.
export type Answer = {
	body: string;
	acceptLabel: string | null;
	acceptLink: string | null;
	dismissLabel: string;
};
