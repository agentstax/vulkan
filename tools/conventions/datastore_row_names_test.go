package conventions

// Walks every type declaration in the datastore packages under pkg/ and
// refuses names ending in "Data": scan structs are named for the table they
// scan plus "Row" (CONVENTIONS.md ## Package layout), and the "Data" suffix
// belongs to the public read-models.

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

func TestDatastoreTypeNamesNeverEndInData(t *testing.T) {
	root := repoRoot(t)

	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if !strings.Contains(filepath.ToSlash(path), "/datastore/") {
			return nil
		}

		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok || !strings.HasSuffix(spec.Name.Name, "Data") {
				return true
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				relative = path
			}
			position := relative + ":" + strconv.Itoa(fileSet.Position(spec.Pos()).Line)
			t.Errorf("%s declares type %s -- name a datastore scan struct for the table it scans plus Row", position, spec.Name.Name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
