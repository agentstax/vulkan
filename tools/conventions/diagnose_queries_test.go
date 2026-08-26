package conventions

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// A declared diagnose query's placeholders are filled from the attributes on
// the reader's own log line, so a placeholder naming something the ## Logging
// attribute registry does not list can never be filled. The registry stays
// prose in CONVENTIONS.md -- this walk is what makes it binding.
func TestDiagnosePlaceholdersNameRegisteredAttributes(t *testing.T) {
	registered := registeredAttributes(t)

	declared := declaredQueries()
	if len(declared) == 0 {
		t.Fatal("no declared queries reached the walk -- check the import list in conventions.go")
	}

	for code, queries := range declared {
		for _, query := range queries {
			for _, name := range query.Placeholders() {
				if !registered(name) {
					t.Errorf("%s query %q substitutes {%s}, which the ### Attributes table does not list", code, query.Label, name)
				}
			}
		}
	}
}

// The ## SQL ban on SELECT * covers declared queries too: a reader pastes
// these into psql, and a star widens silently with the table.
func TestDiagnoseQueriesNameTheirColumns(t *testing.T) {
	star := regexp.MustCompile(`(?i)SELECT\s+\*`)

	for code, queries := range declaredQueries() {
		for _, query := range queries {
			if star.MatchString(query.Sql) {
				t.Errorf("%s query %q selects *, name the columns instead", code, query.Label)
			}
		}
	}
}

// The parse above silently passes if CONVENTIONS.md is reformatted out from
// under it, so this pins what a healthy parse looks like.
func TestRegisteredAttributesParse(t *testing.T) {
	registered := registeredAttributes(t)

	for _, name := range []string{"topic", "topic_id", "group_id", "message_id", "low", "high", "build_version"} {
		if !registered(name) {
			t.Errorf("the ### Attributes table parsed without %q", name)
		}
	}
	if registered("not_an_attribute") {
		t.Error("the parse admits a name the table does not list")
	}
	// the <verb>_count row is a pattern, not a literal name
	if !registered("swept_count") {
		t.Error("the parse rejects a <verb>_count name")
	}
}

// ***************
// *** HELPERS ***
// ***************

// declaredQueries is every diagnose query in the registry, keyed by code.
// Metrics declare none.
func declaredQueries() map[string][]*diagnostic.Query {
	queries := map[string][]*diagnostic.Query{}
	for _, registered := range diagnostic.Errors() {
		if len(registered.Queries) > 0 {
			queries[registered.Code] = registered.Queries
		}
	}
	for _, registered := range diagnostic.Events() {
		if len(registered.Queries) > 0 {
			queries[registered.Code] = registered.Queries
		}
	}
	return queries
}

// attributeRow matches one row of the ### Attributes table: the name (or the
// comma-separated pair the "low, high" row carries), then the two spaces that
// start its description. Continuation lines indent past the name column and
// so never match.
var attributeRow = regexp.MustCompile(`^ {6}([a-z_<>]+(?:, [a-z_<>]+)*) {2,}\S`)

// registeredAttributes reads the ### Attributes table out of CONVENTIONS.md
// and reports whether a name appears in it. A row written <verb>_count is a
// pattern, so it registers the suffix rather than the literal text.
func registeredAttributes(t *testing.T) func(string) bool {
	t.Helper()

	source, err := os.ReadFile(filepath.Join(repoRoot(t), "CONVENTIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	// the heading on its own line, never a mention of it in prose above
	_, rest, found := strings.Cut(string(source), "\n### Attributes\n")
	if !found {
		t.Fatal("CONVENTIONS.md has no ### Attributes section")
	}
	table, _, _ := strings.Cut(rest, "\n### ")

	names := map[string]bool{}
	suffixes := []string{}
	for _, line := range strings.Split(table, "\n") {
		match := attributeRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		for _, name := range strings.Split(match[1], ", ") {
			if !strings.HasPrefix(name, "<") {
				names[name] = true
				continue
			}
			_, suffix, _ := strings.Cut(name, ">")
			suffixes = append(suffixes, suffix)
		}
	}

	return func(name string) bool {
		if names[name] {
			return true
		}
		for _, suffix := range suffixes {
			if strings.HasSuffix(name, suffix) {
				return true
			}
		}
		return false
	}
}
