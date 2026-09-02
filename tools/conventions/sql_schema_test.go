package conventions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// schemaVerb is how every SQL literal spells the schema: Sprintf verb [1],
// filled from the datastore's own Schema. Tables follow as [2], [3].
const schemaVerb = "%[1]s"

// Every table a SQL literal names is written %[1]s.<name>. An unqualified name
// is not a visible failure -- the pool's search_path ends in public, so the
// statement resolves against whatever another installation left there, reading
// or dropping its rows instead of its own. Nothing else catches that: the arity
// is still valid, so the compiler and `go vet` pass, and no unit test runs SQL.
//
// The walk sees names a keyword introduces. A name reaching Postgres as a bind
// parameter or inside a quoted string is qualified in Go at its call site and
// is outside this reach.
func TestSqlLiteralsQualifyTheirTables(t *testing.T) {
	walked := 0

	for _, literal := range sqlLiterals(t) {
		for _, reference := range relationsNamed(literal.Text) {
			walked++
			schema, name, qualified := strings.Cut(reference, ".")
			switch {
			case !qualified:
				t.Errorf("%s names %s unqualified, write %s.%s", literal.Position, reference, schemaVerb, reference)
			case schema != schemaVerb:
				t.Errorf("%s qualifies %s with %s, not %s", literal.Position, reference, schema, schemaVerb)
			case strings.Contains(name, "."):
				t.Errorf("%s qualifies %s more than once -- the schema is one verb, [1]", literal.Position, reference)
			}
		}
	}

	if walked == 0 {
		t.Fatal("no relation reference reached the walk -- check relationReference against the SQL literals")
	}
}

// ***************
// *** HELPERS ***
// ***************

// sqlLiteral is one statement the library runs, with the file:line its
// literal opens on.
type sqlLiteral struct {
	Text     string
	Position string
}

// sqlLiterals is every SQL literal in the library and the CLI. The ## SQL rule
// gives each one a `-- vulkan: <package>.<method>` first line, which is what
// separates a statement from any other backtick string.
func sqlLiterals(t *testing.T) []sqlLiteral {
	t.Helper()
	root := repoRoot(t)

	var literals []sqlLiteral
	for _, tree := range []string{"pkg", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			fileSet := token.NewFileSet()
			parsed, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				relative = path
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				basic, ok := node.(*ast.BasicLit)
				if !ok || basic.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(basic.Value)
				if err != nil || !strings.Contains(text, "-- vulkan:") {
					return true
				}
				position := relative + ":" + strconv.Itoa(fileSet.Position(basic.Pos()).Line)
				literals = append(literals, sqlLiteral{Text: text, Position: position})
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return literals
}

// relationReference matches the token naming a relation a statement reads
// from, writes to, or creates. A subquery opens with ( and so never matches
// the name class.
var relationReference = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INSERT\s+INTO|INTO|UPDATE` +
	`|CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?|DROP\s+TABLE(?:\s+IF\s+EXISTS)?` +
	`|ALTER\s+TABLE|REFERENCES|PARTITION\s+OF)\s+([A-Za-z_%][\w%\[\].]*)`)

// indexTarget matches the table CREATE INDEX builds on. The index's own name
// is skipped deliberately: an index cannot carry a schema and lands in its
// table's.
var indexTarget = regexp.MustCompile(`(?i)\bCREATE\s+(?:UNIQUE\s+)?INDEX(?:\s+IF\s+NOT\s+EXISTS)?\s+\S+\s+ON\s+([A-Za-z_%][\w%\[\].]*)`)

// commonTableExpression matches a WITH clause's names, which read like tables
// and are not.
var commonTableExpression = regexp.MustCompile(`(?i)(?:WITH|,)\s+(\w+)\s+AS\s*(?:MATERIALIZED\s*)?\(`)

// sqlComment matches a comment to its line end.
var sqlComment = regexp.MustCompile(`--[^\n]*`)

// clauseKeywords follow a matched keyword without naming anything: DO UPDATE
// SET, FOR UPDATE SKIP LOCKED.
var clauseKeywords = map[string]bool{
	"set": true, "skip": true, "update": true, "select": true, "values": true,
	"conflict": true, "nothing": true, "of": true, "only": true, "locked": true,
}

// relationsNamed lists the relations one statement names, spelled as the
// statement spells them so the caller can check the qualifier. Catalog tables
// and a WITH clause's own names are not vulkan's to qualify.
func relationsNamed(sql string) []string {
	body := sqlComment.ReplaceAllString(sql, "")

	expressions := map[string]bool{}
	for _, match := range commonTableExpression.FindAllStringSubmatch(body, -1) {
		expressions[strings.ToLower(match[1])] = true
	}

	names := []string{}
	for _, pattern := range []*regexp.Regexp{relationReference, indexTarget} {
		for _, match := range pattern.FindAllStringSubmatch(body, -1) {
			name := match[1]
			lower := strings.ToLower(name)
			if expressions[lower] || strings.HasPrefix(lower, "pg_") || clauseKeywords[lower] {
				continue
			}
			names = append(names, name)
		}
	}
	return names
}
