package diagnostic

import (
	"regexp"
	"slices"
	"strings"
)

// placeholderPattern matches one {attribute_name} placeholder -- the log
// attribute keys the condition's own line already carries.
var placeholderPattern = regexp.MustCompile(`\{[a-z][a-z0-9_]*\}`)

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
	found := placeholderPattern.FindAllString(q.Sql, -1)

	names := make([]string, 0, len(found))
	for _, placeholder := range found {
		name := strings.Trim(placeholder, "{}")
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}
