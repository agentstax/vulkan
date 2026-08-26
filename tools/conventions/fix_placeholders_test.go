package conventions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// A fix is one string for every raise site of its code, so a placeholder only
// earns its place if EVERY site can attach it -- one that cannot renders
// literally on the line an operator reads.
//
// Diagnose queries are exempt: a declaration carries an ordered SET, so a
// name-keyed and an id-keyed query can sit side by side.
func TestFixPlaceholdersAreAttachedAtEveryRaiseSite(t *testing.T) {
	substituted := map[string][]string{}
	for _, registered := range diagnostic.Errors() {
		if placeholders := registered.FixPlaceholders(); len(placeholders) > 0 {
			substituted[registered.Code] = placeholders
		}
	}
	if len(substituted) == 0 {
		t.Fatal("no fix declares a placeholder -- check the import list in conventions.go")
	}

	sites := declaredRaiseSites(t)
	for code, placeholders := range substituted {
		raised := sites[code]
		if len(raised) == 0 {
			t.Errorf("%s substitutes %v but the walk found no raise site for it", code, placeholders)
			continue
		}
		for _, site := range raised {
			for _, name := range placeholders {
				if !slices.Contains(site.attached, name) {
					t.Errorf("%s: %s raises it without {%s}, which its fix substitutes", code, site.where, name)
				}
			}
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

type declaredRaise struct {
	attached []string
	where    string // file:line
}

// declaredRaiseSites keys every `return <declared Err>` under pkg/ by the code
// the returned variable declares. A use that is not returned -- an errors.Is
// comparison -- is not a raise and never reaches the map.
func declaredRaiseSites(t *testing.T) map[string][]declaredRaise {
	t.Helper()

	codes := declaredErrorVariables(t)
	fileSet := token.NewFileSet()

	sites := map[string][]declaredRaise{}
	err := filepath.WalkDir(filepath.Join(repoRoot(t), "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
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
			returned, ok := node.(*ast.ReturnStmt)
			if !ok {
				return true
			}

			for _, result := range returned.Results {
				variable, attached := unwindRaise(result)
				code, declared := codes[variable]
				if !declared {
					continue
				}
				where := fileSet.Position(result.Pos())
				sites[code] = append(sites[code], declaredRaise{
					attached: attached,
					where:    filepath.Base(where.Filename) + ":" + strconv.Itoa(where.Line),
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sites
}

// unwindRaise walks a returned expression down its With/Wrap chain, collecting
// the names every With attached. Anything but a declared error unwinds to "".
func unwindRaise(expression ast.Expr) (string, []string) {
	attached := []string{}
	for {
		switch typed := expression.(type) {
		case *ast.CallExpr:
			selector, ok := typed.Fun.(*ast.SelectorExpr)
			if !ok {
				return "", nil
			}
			if selector.Sel.Name == "With" {
				attached = append(attached, literalNames(typed.Args)...)
			} else if selector.Sel.Name != "Wrap" {
				return "", nil
			}
			expression = selector.X
		case *ast.SelectorExpr:
			return typed.Sel.Name, attached
		case *ast.Ident:
			return typed.Name, attached
		default:
			return "", nil
		}
	}
}

// literalNames reads the name of each name/value pair. A name that is not a
// string literal is skipped: nothing static can tell what it spells.
func literalNames(pairs []ast.Expr) []string {
	names := []string{}
	for i := 0; i < len(pairs); i += 2 {
		literal, ok := pairs[i].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			continue
		}
		name, err := strconv.Unquote(literal.Value)
		if err != nil {
			continue
		}
		names = append(names, name)
	}
	return names
}

// declaredErrorVariables maps each `var ErrX = diagnostic.NewError("VKnnnn"` to
// its code. The registry knows the codes but not the variable name a raise
// site spells.
func declaredErrorVariables(t *testing.T) map[string]string {
	t.Helper()

	fileSet := token.NewFileSet()
	codes := map[string]string{}
	err := filepath.WalkDir(filepath.Join(repoRoot(t), "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
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
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				if code := declaredErrorCode(value.Values[0]); code != "" {
					codes[value.Names[0].Name] = code
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return codes
}

// declaredErrorCode returns the code a NewError initializer declares, walking
// past any Diagnose chained onto it.
func declaredErrorCode(expression ast.Expr) string {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return ""
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if selector.Sel.Name == "Diagnose" {
		return declaredErrorCode(selector.X)
	}
	if selector.Sel.Name != "NewError" || len(call.Args) == 0 {
		return ""
	}

	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return ""
	}
	code, err := strconv.Unquote(literal.Value)
	if err != nil {
		return ""
	}
	return code
}
