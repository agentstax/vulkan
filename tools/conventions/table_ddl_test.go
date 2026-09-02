package conventions

// Walks every baseline CREATE TABLE literal under pkg/ plus the per-topic
// table-name funcs in pkg/topic and enforces the mechanical half of
// CONVENTIONS.md ## Tables naming rules [0611][0613]: table names end in a
// known kind, TIMESTAMPTZ columns end _at/_after, duration columns are
// BIGINT nanoseconds ending _ns. Judgment rules (root wording, prefix
// choice) stay review-time.

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

type tableStatement struct {
	Position string             // file:line of the CREATE TABLE line
	Name     string             // "" when the name is a %s placeholder (per-topic)
	Columns  []columnDefinition // empty for PARTITION OF statements
}

type columnDefinition struct {
	Position string // file:line of the column's own line
	Name     string
	Type     string
}

// tableKinds is [0611]'s registry: the trailing word every table name ends in.
var tableKinds = map[string]bool{
	"config":   true,
	"log":      true,
	"queue":    true,
	"lease":    true,
	"instance": true,
	"cursor":   true,
	"head":     true,
}

// tableKindExceptions are [0611]'s deliberate leftovers outside the kind set.
var tableKindExceptions = map[string]bool{
	"idempotency_key": true,
}

func TestTableNamesEndInAKnownKind(t *testing.T) {
	names := make(map[string]string) // name -> position

	for _, statement := range baselineTableStatements(t) {
		if statement.Name != "" {
			names[statement.Name] = statement.Position
		}
	}
	for name, position := range perTopicTableNames(t) {
		names[name] = position
	}

	for name, position := range names {
		if tableKindExceptions[name] {
			continue
		}
		kind := name[strings.LastIndex(name, "_")+1:]
		if !tableKinds[kind] {
			t.Errorf("%s table %q ends in %q, not a known kind [0611]", position, name, kind)
		}
	}
}

func TestTimestamptzColumnsEndAtOrAfter(t *testing.T) {
	for _, statement := range baselineTableStatements(t) {
		for _, column := range statement.Columns {
			if column.Type != "TIMESTAMPTZ" {
				continue
			}
			if !strings.HasSuffix(column.Name, "_at") && !strings.HasSuffix(column.Name, "_after") {
				t.Errorf("%s TIMESTAMPTZ column %q must end _at or _after [0613]", column.Position, column.Name)
			}
		}
	}
}

// durationWord marks a column as duration-shaped by a whole underscore-
// separated word, never a substring (settled_head contains "ttl").
var durationWord = regexp.MustCompile(`(^|_)(ttl|timeout|duration)(_|$)`)

func TestDurationColumnsAreBigintNs(t *testing.T) {
	for _, statement := range baselineTableStatements(t) {
		for _, column := range statement.Columns {
			if strings.HasSuffix(column.Name, "_ns") && column.Type != "BIGINT" {
				t.Errorf("%s column %q ends _ns but is %s, not BIGINT [0613]", column.Position, column.Name, column.Type)
			}
			if durationWord.MatchString(column.Name) && !strings.HasSuffix(column.Name, "_ns") {
				t.Errorf("%s duration column %q must end _ns [0613]", column.Position, column.Name)
			}
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

// constraintKeywords open the table-level lines a column parse skips.
var constraintKeywords = map[string]bool{
	"PRIMARY":    true,
	"UNIQUE":     true,
	"CHECK":      true,
	"FOREIGN":    true,
	"CONSTRAINT": true,
}

func baselineTableStatements(t *testing.T) []tableStatement {
	t.Helper()
	root := repoRoot(t)

	var statements []tableStatement
	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
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
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING || !strings.Contains(literal.Value, "CREATE TABLE") {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			startLine := fileSet.Position(literal.Pos()).Line
			statements = append(statements, parseCreateTable(text, relative, startLine))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return statements
}

// schemaQualifier is what every SQL literal writes ahead of a table name --
// Sprintf verb [1] is the schema on every statement. Trimming it is what keeps
// a shared table's name visible to the kind check: left on, the name still
// holds a %% and reads as a per-topic placeholder, so the check walks nothing.
const schemaQualifier = "%[1]s."

// parseCreateTable reads one CREATE TABLE literal into its name and column
// lines. startLine is the literal's opening line in its file, so each parsed
// line can carry a real file:line.
func parseCreateTable(text string, file string, startLine int) tableStatement {
	statement := tableStatement{Position: file + ":" + strconv.Itoa(startLine)}
	lines := strings.Split(text, "\n")

	// the CREATE line names the table; a %s placeholder means per-topic --
	// those names are checked through pkg/topic's funcs instead
	createIndex := -1
	for i, line := range lines {
		after, found := strings.CutPrefix(strings.TrimSpace(line), "CREATE TABLE IF NOT EXISTS ")
		if !found {
			continue
		}
		statement.Position = file + ":" + strconv.Itoa(startLine+i)
		name := strings.Fields(after)[0]
		name = strings.TrimSuffix(name, "(")
		name = strings.TrimPrefix(name, schemaQualifier)
		if !strings.Contains(name, "%") {
			statement.Name = name
		}
		createIndex = i
		break
	}
	if createIndex < 0 || strings.Contains(text, "PARTITION OF") {
		return statement
	}

	for i, line := range lines[createIndex+1:] {
		if index := strings.Index(line, "--"); index >= 0 {
			line = line[:index]
		}
		line = strings.TrimSuffix(strings.TrimSpace(line), ",")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ")") {
			break
		}

		fields := strings.Fields(line)
		if len(fields) < 2 || constraintKeywords[fields[0]] {
			continue
		}
		statement.Columns = append(statement.Columns, columnDefinition{
			Position: file + ":" + strconv.Itoa(startLine+createIndex+1+i),
			Name:     fields[0],
			Type:     fields[1],
		})
	}
	return statement
}

// perTopicTableNames reads pkg/topic's table-name funcs: every Sprintf
// format shaped <name>_%d is a per-topic table name.
func perTopicTableNames(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	path := filepath.Join(root, "pkg", "topic", "tables.go")

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	perTopicName := regexp.MustCompile(`^([a-z_]+)_%d$`)
	names := make(map[string]string)
	ast.Inspect(parsed, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		match := perTopicName.FindStringSubmatch(text)
		if match == nil {
			return true
		}
		names[match[1]] = "pkg/topic/tables.go:" + strconv.Itoa(fileSet.Position(literal.Pos()).Line)
		return true
	})
	return names
}
