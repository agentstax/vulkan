package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// The real registry is what ships, so the export is asserted against it
// rather than against a fixture: every declared code must reach the site.
func TestNewExportCoversTheRegistry(t *testing.T) {
	export := build(t)

	wanted := len(diagnostic.Errors()) + len(diagnostic.Events()) + len(diagnostic.Metrics())
	if len(export.Codes) != wanted {
		t.Fatalf("export carries %d codes, want %d", len(export.Codes), wanted)
	}
	for _, declared := range diagnostic.Errors() {
		record, found := export.Codes[declared.Code]
		if !found {
			t.Fatalf("%s is missing from the export", declared.Code)
		}
		if record.Problem != declared.Problem || record.Fix != declared.Fix {
			t.Errorf("%s exports %q/%q, want %q/%q", declared.Code, record.Problem, record.Fix, declared.Problem, declared.Fix)
		}
		if record.Kind != "error" {
			t.Errorf("%s exports kind %q, want \"error\"", declared.Code, record.Kind)
		}
	}
}

// The queries are the part no page can restate by hand, and their
// placeholders travel with them so the site never re-parses the SQL.
func TestNewExportCarriesQueriesAndPlaceholders(t *testing.T) {
	export := build(t)

	record, found := export.Codes["VK0029"]
	if !found {
		t.Fatal("VK0029 is missing from the export")
	}
	if len(record.Queries) == 0 {
		t.Fatal("VK0029 exports no queries")
	}
	first := record.Queries[0]
	if !strings.Contains(first.Sql, "{topic_id}") {
		t.Errorf("VK0029's first query lost its placeholders: %q", first.Sql)
	}
	if !slices.Contains(first.Placeholders, "topic_id") {
		t.Errorf("VK0029's first query lists placeholders %v, want topic_id among them", first.Placeholders)
	}
}

// A declaration with nothing to look at exports no queries key at all, so
// the site renders no section rather than an empty one.
func TestNewExportOmitsAbsentParts(t *testing.T) {
	export := build(t)

	encoded, err := json.Marshal(export.Codes["VK0001"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "queries") {
		t.Errorf("VK0001 exports a queries key with none declared: %s", encoded)
	}
	if strings.Contains(string(encoded), "message") {
		t.Errorf("VK0001 is an error but exports an event's message: %s", encoded)
	}
}

// A map would drop one of two records sharing a code, and a dropped code is
// a page with no data behind it.
func TestNewExportRefusesACodeTwoKindsClaim(t *testing.T) {
	declared := diagnostic.Errors()[0]
	colliding := &diagnostic.DiagnosticEvent{Code: declared.Code, Message: "a message"}

	_, err := NewExport([]*diagnostic.DiagnosticError{declared}, []*diagnostic.DiagnosticEvent{colliding}, nil)
	if err == nil {
		t.Fatal("NewExport accepted a code declared as two kinds")
	}
	if !strings.Contains(err.Error(), declared.Code) {
		t.Errorf("the error does not name the code: %v", err)
	}
}

// ***************
// *** HELPERS ***
// ***************

func build(t *testing.T) *Export {
	t.Helper()

	export, err := NewExport(diagnostic.Errors(), diagnostic.Events(), diagnostic.Metrics())
	if err != nil {
		t.Fatal(err)
	}
	return export
}
