package diagnostic

import (
	"strings"
)

// Query is one declared diagnose query: the label names what the query
// answers, the SQL answers it against the reader's own database. The library
// never runs it -- the fix says what to change, a query says what to look at.
type Query struct {
	Label string
	Sql   string
}

// NewQuery declares one diagnose query. Declaration happens at package init,
// so structural mistakes panic instead of returning an error nothing would
// check.
func NewQuery(label string, sql string) *Query {
	sql = strings.TrimSpace(sql)

	if label == "" {
		panic("query label must not be empty")
	}
	if sql == "" {
		panic("query sql must not be empty: " + label)
	}

	return &Query{Label: label, Sql: sql}
}

// Placeholders lists each placeholder's attribute name once, in
// first-appearance order.
func (q *Query) Placeholders() []string {
	return placeholderNames(q.Sql)
}
