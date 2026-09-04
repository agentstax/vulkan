package conventions

// Walks every exported type declaration under pkg/ and refuses the
// representation-only "Data" and "Info" suffixes. Public type suffixes name
// semantic roles (CONVENTIONS.md ## Naming & terminology).

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

func TestExportedTypeNamesUseSemanticRoles(t *testing.T) {
	root := repoRoot(t)

	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok || !ast.IsExported(spec.Name.Name) {
				return true
			}
			if !strings.HasSuffix(spec.Name.Name, "Data") && !strings.HasSuffix(spec.Name.Name, "Info") {
				return true
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				relative = path
			}
			position := relative + ":" + strconv.Itoa(fileSet.Position(spec.Pos()).Line)
			t.Errorf("%s declares type %s -- exported type suffixes name semantic roles; Data and Info do not", position, spec.Name.Name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
