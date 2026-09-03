package conventions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/metrics"
)

func TestProblemTenseFollowsRecovery(t *testing.T) {
	for _, registered := range diagnostic.Errors() {
		startsCouldNot := strings.HasPrefix(registered.Problem, "could not ")

		switch registered.Recovery {
		case diagnostic.RecoveryTransient:
			if !startsCouldNot {
				t.Errorf(`%s is Transient but its problem does not start "could not ": %q`, registered.Code, registered.Problem)
			}
		case diagnostic.RecoveryPermanent:
			if startsCouldNot {
				t.Errorf(`%s is Permanent but its problem starts "could not ": %q`, registered.Code, registered.Problem)
			}
		}
	}
}

// bannedWords is shared with the plain-raise-string walk in
// plain_errors_test.go -- one list, both walks.
var bannedWords = regexp.MustCompile(`(?i)\b(failed|invalid|bad|illegal|unable|unknown|error|please|sorry)\b`)

func TestProblemAvoidsBannedWords(t *testing.T) {
	for _, registered := range diagnostic.Errors() {
		if match := bannedWords.FindString(registered.Problem); match != "" {
			t.Errorf("%s problem contains banned word %q: %q", registered.Code, match, registered.Problem)
		}
		if strings.Contains(registered.Problem, "!") {
			t.Errorf("%s problem contains an exclamation point: %q", registered.Code, registered.Problem)
		}
	}
}

func TestLogEventMessageAvoidsBannedWords(t *testing.T) {
	for _, registered := range diagnostic.Events() {
		if match := bannedWords.FindString(registered.Message); match != "" {
			t.Errorf("%s message contains banned word %q: %q", registered.Code, match, registered.Message)
		}
		if strings.Contains(registered.Message, "!") {
			t.Errorf("%s message contains an exclamation point: %q", registered.Code, registered.Message)
		}
	}
}

func TestMetricDescriptionAvoidsBannedWords(t *testing.T) {
	for _, registered := range diagnostic.Metrics() {
		if match := bannedWords.FindString(registered.Description); match != "" {
			t.Errorf("%s description contains banned word %q: %q", registered.Code, match, registered.Description)
		}
		if strings.Contains(registered.Description, "!") {
			t.Errorf("%s description contains an exclamation point: %q", registered.Code, registered.Description)
		}
	}
}

// A metric declaration's kind and unit are plain text on the diagnostic
// side; this walk holds every declaration to the pkg/metrics vocabulary.
func TestMetricDeclarationsCarryMetricsVocabulary(t *testing.T) {
	for _, registered := range diagnostic.Metrics() {
		if !strings.HasPrefix(registered.Name, metrics.MetricNameReservedPrefix) {
			t.Errorf("%s name %q must start with %q", registered.Code, registered.Name, metrics.MetricNameReservedPrefix)
		}
		if err := metrics.MetricKind(registered.Kind).Validate(); err != nil {
			t.Errorf("%s: %v", registered.Code, err)
		}
		if err := metrics.MetricUnit(registered.Unit).Validate(); err != nil {
			t.Errorf("%s: %v", registered.Code, err)
		}
	}
}

// Codes live at roots (CONVENTIONS.md ## Package layout): every
// NewDiagnosticError / NewDiagnosticEvent / NewDiagnosticMetric call in the
// library initializes an exported var in a root's errors.go, events.go, or
// metrics.go, or under pkg/common. The same walk proves the import lists in
// conventions.go and tools/codeexport/main.go are complete: a code the
// registry this binary sees does not hold, or a declaring package the
// exporter does not link, would leave a page with no record behind it.
func TestCodesDeclaredAtRoots(t *testing.T) {
	registered := map[string]bool{}
	for _, entry := range diagnostic.Errors() {
		registered[entry.Code] = true
	}
	for _, entry := range diagnostic.Events() {
		registered[entry.Code] = true
	}
	for _, entry := range diagnostic.Metrics() {
		registered[entry.Code] = true
	}

	root := repoRoot(t)
	exported := moduleImports(t, filepath.Join(root, "tools", "codeexport", "main.go"))
	linked := moduleImports(t, filepath.Join(root, "tools", "conventions", "conventions.go"))
	walked := 0

	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)

		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		held := map[*ast.CallExpr]bool{}
		for _, declaration := range parsed.Decls {
			generic, ok := declaration.(*ast.GenDecl)
			if !ok || generic.Tok != token.VAR {
				continue
			}
			for _, spec := range generic.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if i >= len(value.Values) {
						break
					}
					code, declares := declaredCode(value.Values[i])
					if !declares {
						continue
					}
					walked++
					ast.Inspect(value.Values[i], func(node ast.Node) bool {
						if call, ok := node.(*ast.CallExpr); ok && declaresCode(call) {
							held[call] = true
						}
						return true
					})
					position := relative + ":" + strconv.Itoa(fileSet.Position(value.Pos()).Line)
					switch {
					case !isCodeHome(relative):
						t.Errorf("%s declares %s outside a root's errors.go, events.go, or metrics.go", position, code)
					case !ast.IsExported(name.Name):
						t.Errorf("%s declares %s under unexported name %s", position, code, name.Name)
					}
					if !registered[code] {
						t.Errorf("%s declares %s but the registry misses it -- add its package to conventions.go", position, code)
					}
					importPath := modulePath + "/" + filepath.ToSlash(filepath.Dir(relative))
					if !exported[importPath] {
						t.Errorf("%s declares %s but tools/codeexport/main.go does not link %s", position, code, importPath)
					}
					if !linked[importPath] {
						t.Errorf("%s declares %s but tools/conventions/conventions.go does not link %s", position, code, importPath)
					}
				}
			}
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && declaresCode(call) && !held[call] {
				t.Errorf("%s:%d declares a code that no package-level var holds", relative, fileSet.Position(call.Pos()).Line)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked == 0 {
		t.Fatal("no code declaration reached the walk -- check declaredCode against the roots")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path unavailable")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// ***************
// *** HELPERS ***
// ***************

// declaresCode reports whether a call is one of the three declaration
// constructors.
func declaresCode(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || qualifier.Name != "diagnostic" {
		return false
	}
	switch selector.Sel.Name {
	case "NewDiagnosticError", "NewDiagnosticEvent", "NewDiagnosticMetric":
		return true
	}
	return false
}

// isCodeHome reports whether a repo-relative path is a place a code may be
// declared: pkg/<x>/errors.go, events.go, or metrics.go, or anything under
// pkg/common.
func isCodeHome(relative string) bool {
	if strings.HasPrefix(relative, "pkg/common/") {
		return true
	}
	segments := strings.Split(relative, "/")
	if len(segments) != 3 || segments[0] != "pkg" {
		return false
	}
	switch segments[2] {
	case "errors.go", "events.go", "metrics.go":
		return true
	}
	return false
}

// moduleImports lists the module packages one file imports.
func moduleImports(t *testing.T, path string) map[string]bool {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	imported := map[string]bool{}
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		imported[importPath] = true
	}
	return imported
}
