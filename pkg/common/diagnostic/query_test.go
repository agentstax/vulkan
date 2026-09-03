package diagnostic

import (
	"slices"
	"testing"
)

const deliverySql = `SELECT
	status,
	attempts
FROM exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id = {message_id};`

func TestNewQueryKeepsWhatItWasGiven(t *testing.T) {
	query := NewDiagnosticQuery("the delivery row", deliverySql)
	if query.Label != "the delivery row" {
		t.Errorf("label = %q, want %q", query.Label, "the delivery row")
	}
	if query.Sql != deliverySql {
		t.Error("sql was not kept verbatim")
	}
}

// A declaration writes its SQL on the line after the opening backtick, so
// the leading newline is the literal's shape, not the query's.
func TestNewQueryTrimsTheLiteralsOwnWhitespace(t *testing.T) {
	query := NewDiagnosticQuery("the delivery row", "\n"+deliverySql+"\n\t")
	if query.Sql != deliverySql {
		t.Errorf("sql = %q, want it trimmed", query.Sql)
	}
}

func TestNewQueryPanicsOnStructuralMistakes(t *testing.T) {
	cases := map[string]struct {
		label string
		sql   string
	}{
		"no label":        {"", "SELECT 1"},
		"no sql":          {"the delivery row", ""},
		"only whitespace": {"the delivery row", "\n\t"},
	}
	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("NewDiagnosticQuery returned instead of panicking")
				}
			}()
			NewDiagnosticQuery(one.label, one.sql)
		})
	}
}

// A JSONB literal is written '{"key": 1}', so its braces must not read as a
// placeholder the reader is asked to fill in.
func TestQueryPlaceholdersSkipsJsonbLiterals(t *testing.T) {
	query := NewDiagnosticQuery("messages carrying the key", `SELECT id FROM message_log_{topic_id} WHERE payload @> '{"tenant": 1}'`)
	if got := query.Placeholders(); !slices.Equal(got, []string{"topic_id"}) {
		t.Errorf("placeholders = %v, want [topic_id]", got)
	}
}

func TestQueryPlaceholders(t *testing.T) {
	cases := map[string]struct {
		sql  string
		want []string
	}{
		"in first-appearance order": {deliverySql, []string{"topic_id", "group_id", "message_id"}},
		"each listed once":          {"{topic_id} {group_id} {topic_id}", []string{"topic_id", "group_id"}},
		"none to substitute":        {"SELECT migration_version FROM migration_log", []string{}},
	}
	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			got := NewDiagnosticQuery("a query", one.sql).Placeholders()
			if !slices.Equal(got, one.want) {
				t.Errorf("placeholders = %v, want %v", got, one.want)
			}
		})
	}
}

func TestDiagnoseAttachesToTheRegisteredDeclaration(t *testing.T) {
	query := NewDiagnosticQuery("the delivery row", deliverySql)

	declared := NewDiagnosticError("VK9001", RecoveryPermanent, "a condition with state to look at", "do the thing").
		Diagnose(query)
	if len(declared.Queries) != 1 || declared.Queries[0] != query {
		t.Fatalf("queries = %v, want the declared one", declared.Queries)
	}

	// the registry holds the same pointer, so every surface reads the
	// queries back rather than a bare declaration
	registered := false
	for _, listed := range Errors() {
		if listed.Code == "VK9001" {
			registered = len(listed.Queries) == 1
		}
	}
	if !registered {
		t.Error("the registered declaration carries no queries")
	}
}

func TestDiagnoseOnAnEvent(t *testing.T) {
	declared := NewDiagnosticEvent("VK9002", "a thing happened", "").
		Diagnose(NewDiagnosticQuery("the row it wrote", deliverySql))
	if len(declared.Queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(declared.Queries))
	}
}

func TestDiagnosePanics(t *testing.T) {
	cases := map[string]func(){
		"with no queries": func() {
			NewDiagnosticError("VK9003", RecoveryPermanent, "a condition", "").Diagnose()
		},
		"when already declared": func() {
			query := NewDiagnosticQuery("the delivery row", deliverySql)
			NewDiagnosticError("VK9004", RecoveryPermanent, "a condition", "").Diagnose(query).Diagnose(query)
		},
	}
	for name, declare := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Diagnose returned instead of panicking")
				}
			}()
			declare()
		})
	}
}
