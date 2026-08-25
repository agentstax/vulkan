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

// A raised error's values render as name/value pairs on the line an operator
// reads, so they answer to the same ## Logging attribute registry the log
// call sites do. An unregistered name is a second spelling of a concept that
// already has one -- and nothing can fill a diagnose query from it.
func TestRaisedValuesNameRegisteredAttributes(t *testing.T) {
	registered := registeredAttributes(t)

	raised := raisedValueNames(t)
	if len(raised) == 0 {
		t.Fatal("no With calls reached the walk")
	}

	for _, name := range raised {
		if !registered(name.attribute) {
			t.Errorf("%s raises {%s}, which the ### Attributes table does not list", name.where, name.attribute)
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

// raisedValue is one name/value pair attached at a raise site.
type raisedValue struct {
	attribute string
	where     string // file:line
}

// raisedValueNames reads every Err*.With(...) call under pkg/ and returns the
// attribute name of each pair. A pair whose name is not a literal string is
// skipped: nothing static can tell what it spells.
func raisedValueNames(t *testing.T) []raisedValue {
	t.Helper()

	root := repoRoot(t)
	fileSet := token.NewFileSet()

	names := []raisedValue{}
	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "With" || !raisesDeclaredError(selector.X) {
				return true
			}

			// pairs are name, value, name, value -- only the names are checked
			for i := 0; i < len(call.Args); i += 2 {
				literal, ok := call.Args[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				where := fileSet.Position(literal.Pos())
				names = append(names, raisedValue{
					attribute: name,
					where:     filepath.Base(where.Filename) + ":" + strconv.Itoa(where.Line),
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// raisesDeclaredError reports whether a With call sits on a declared error
// variable -- ErrX.With(...) or package.ErrX.With(...) -- which is what
// separates it from every other With in the tree.
func raisesDeclaredError(receiver ast.Expr) bool {
	switch typed := receiver.(type) {
	case *ast.Ident:
		return strings.HasPrefix(typed.Name, "Err")
	case *ast.SelectorExpr:
		return strings.HasPrefix(typed.Sel.Name, "Err")
	}
	return false
}
