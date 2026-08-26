// DiagnoseQuery is one declared query behind a VK code: the label states what
// the query answers, the sql carries {attribute_name} placeholders for the
// values the reader's own log line supplies, and placeholders names them. The
// library parses the SQL when it exports the declaration, so this component
// renders that list rather than parsing the SQL a second time.
export type DiagnoseQuery = {
	label: string;
	sql: string;
	placeholders: string[];
};
