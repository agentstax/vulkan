// DiagnoseQuery is one declared query behind a VK code: the label states what
// the query answers, the sql carries {attribute_name} placeholders for the
// values the reader's own log line supplies.
export type DiagnoseQuery = {
	label: string;
	sql: string;
};
