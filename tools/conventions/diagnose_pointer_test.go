package conventions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pointerLine is the one line a declaration with diagnose queries carries in
// its doc comment. gopls hover renders the doc comment and the type but not
// the initializer, so the queries are invisible at the surface where callers
// write errors.Is -- the pointer is what carries them there.
const pointerLine = "Diagnose queries: vulkan explain "

func TestDeclarationsWithQueriesPointAtExplain(t *testing.T) {
	declarations := declarationDocComments(t)
	if len(declarations) == 0 {
		t.Fatal("no declarations found -- the walk is checking nothing")
	}

	for _, declared := range declarations {
		want := pointerLine + declared.code
		switch {
		case declared.hasQueries && !strings.Contains(declared.doc, want):
			t.Errorf("%s (%s) declares diagnose queries but its doc comment does not carry %q",
				declared.name, declared.where, want)
		case !declared.hasQueries && strings.Contains(declared.doc, pointerLine):
			t.Errorf("%s (%s) points at diagnose queries it does not declare",
				declared.name, declared.where)
		}
	}
}

// The pointer names the declaration's own code, so a copied line that kept the
// donor's code sends the reader to the wrong page.
func TestDiagnosePointersNameTheirOwnCode(t *testing.T) {
	// CommentGroup.Text strips the // markers, so the line reads bare here.
	for _, declared := range declarationDocComments(t) {
		for _, line := range strings.Split(declared.doc, "\n") {
			pointed, found := strings.CutPrefix(strings.TrimSpace(line), pointerLine)
			if found && strings.TrimSpace(pointed) != declared.code {
				t.Errorf("%s (%s) is %s but points at %q",
					declared.name, declared.where, declared.code, strings.TrimSpace(pointed))
			}
		}
	}
}

type declaration struct {
	name       string
	code       string
	doc        string
	hasQueries bool
	where      string // file:line
}

// declarationDocComments reads every diagnostic.NewError / NewEvent variable
// under pkg/ with its doc comment, and whether the initializer chains a
// Diagnose call.
func declarationDocComments(t *testing.T) []declaration {
	t.Helper()

	root := repoRoot(t)
	fileSet := token.NewFileSet()

	declarations := []declaration{}
	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		parsed, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, node := range parsed.Decls {
			general, ok := node.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				code, declares := declaredCode(value.Values[0])
				if !declares {
					continue
				}

				where := fileSet.Position(value.Pos())
				declarations = append(declarations, declaration{
					name:       value.Names[0].Name,
					code:       code,
					doc:        general.Doc.Text() + value.Doc.Text(),
					hasQueries: chainsDiagnose(value.Values[0]),
					where:      filepath.Base(where.Filename) + ":" + strconv.Itoa(where.Line),
				})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return declarations
}

// ***************
// *** HELPERS ***
// ***************

// declaredCode returns the VK code a NewError or NewEvent call declares. The
// call may sit under a Diagnose chain, so the walk descends to the innermost
// constructor.
func declaredCode(value ast.Expr) (string, bool) {
	code := ""
	ast.Inspect(value, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "NewError" && selector.Sel.Name != "NewEvent") {
			return true
		}
		if len(call.Args) == 0 {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		unquoted, err := strconv.Unquote(literal.Value)
		if err == nil {
			code = unquoted
		}
		return false
	})
	return code, code != ""
}

func chainsDiagnose(value ast.Expr) bool {
	found := false
	ast.Inspect(value, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Diagnose" {
			found = true
			return false
		}
		return true
	})
	return found
}
