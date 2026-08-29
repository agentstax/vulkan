package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

var errTestTopicMissing = diagnostic.NewError("VK9803", diagnostic.Permanent,
	"test topic not found", "register it first").
	Diagnose(
		diagnostic.NewQuery("the topic rows registered under that name", `
SELECT id
FROM topic
WHERE name = '{topic}';`),
		diagnostic.NewQuery("the migration steps this database recorded", `
SELECT migration_version
FROM migration_log;`),
	)

func TestRenderDiagnoseQueriesNamesEveryValueOnce(t *testing.T) {
	var builder strings.Builder
	renderDiagnoseQueries(&builder, errTestTopicMissing.Queries)

	want := "\ndiagnose: fill in topic with your own values\n" +
		"\n  -- the topic rows registered under that name\n" +
		"  SELECT id\n" +
		"  FROM topic\n" +
		"  WHERE name = '{topic}';\n" +
		"\n  -- the migration steps this database recorded\n" +
		"  SELECT migration_version\n" +
		"  FROM migration_log;\n"
	if builder.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", builder.String(), want)
	}
}

func TestRenderDiagnoseQueriesDropsTheSubstitutionLineWithNothingToFillIn(t *testing.T) {
	var builder strings.Builder
	renderDiagnoseQueries(&builder, []*diagnostic.Query{
		diagnostic.NewQuery("every registered topic", "SELECT name FROM topic_config;"),
	})

	want := "\ndiagnose:\n" +
		"\n  -- every registered topic\n" +
		"  SELECT name FROM topic_config;\n"
	if builder.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", builder.String(), want)
	}
}

// Most declarations have nothing to look at, so the section is absent rather
// than empty.
func TestRenderDiagnoseQueriesWritesNothingWhenNoneAreDeclared(t *testing.T) {
	var builder strings.Builder
	renderDiagnoseQueries(&builder, errTestBroker.Queries)
	if builder.String() != "" {
		t.Fatalf("got:\n%s\nwant nothing", builder.String())
	}
}

func TestExplainDocumentCarriesTheDeclaredQueries(t *testing.T) {
	encoded, err := json.Marshal(toErrorExplainDocument(errTestTopicMissing, "register it first"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var document explainDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("output is not one json document: %v\n%s", encoded, err)
	}
	if len(document.Queries) != 2 {
		t.Fatalf("queries = %d, want 2", len(document.Queries))
	}
	first := document.Queries[0]
	if first.Label != "the topic rows registered under that name" {
		t.Errorf("label = %q", first.Label)
	}
	if !strings.HasPrefix(first.Sql, "SELECT id\n") {
		t.Errorf("sql = %q, want it to start at the SELECT", first.Sql)
	}
	if len(first.Placeholders) != 1 || first.Placeholders[0] != "topic" {
		t.Errorf("placeholders = %v, want [topic]", first.Placeholders)
	}
}

// A declaration with no queries leaves the key out rather than carrying an
// empty list, so a reader branches on presence.
func TestExplainDocumentOmitsQueriesWhenNoneAreDeclared(t *testing.T) {
	encoded, err := json.Marshal(toErrorExplainDocument(errTestBroker, "retry the produce call"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "queries") {
		t.Fatalf("queries key present: %s", encoded)
	}
}
