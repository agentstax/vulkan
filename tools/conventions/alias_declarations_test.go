package conventions

// The one-declaration law (CONVENTIONS.md ## Package layout): every exported
// type is declared once, and the only `type X = pkg.X` lines in the repo
// are pkg/vulkan/alias.go. A second alias file is a second place a rename
// has to land; an alias into machinery is a click-through that lands in a
// controller.

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

const modulePath = "github.com/agentstax/vulkan"

// aliasFile is the one file allowed to declare type aliases.
const aliasFile = "pkg/vulkan/alias.go"

func TestAliasesLiveOnlyInVulkan(t *testing.T) {
	root := repoRoot(t)
	walked := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "website":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == aliasFile {
			return nil
		}

		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		walked++
		for _, spec := range typeSpecs(parsed) {
			if spec.Assign.IsValid() {
				t.Errorf("%s:%d declares alias %s -- the only alias file is %s", relative, fileSet.Position(spec.Pos()).Line, spec.Name.Name, aliasFile)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked == 0 {
		t.Fatal("no Go file reached the walk")
	}
}

// pkg/vulkan imports the declaring packages only -- common, datastore, a
// root, or an assembler. An import of a controller or a datastore is the
// tell that vulkan is composing what an assembler should, or aliasing a
// type the machinery floor forbids it to declare. pkg/datastore is
// infrastructure and is not the path this test names.
func TestVulkanImportsNoMachinery(t *testing.T) {
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "pkg", "vulkan", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no Go file under pkg/vulkan")
	}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(path, modulePath+"/") {
				continue
			}
			if strings.HasSuffix(path, "/controller") || strings.Contains(path, "/controller/") {
				t.Errorf("%s imports %s -- vulkan reaches machinery only through an assembler", filepath.Base(file), path)
			}
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

// typeSpecs lists a file's type declarations, grouped or not.
func typeSpecs(parsed *ast.File) []*ast.TypeSpec {
	var specs []*ast.TypeSpec
	for _, declaration := range parsed.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || generic.Tok != token.TYPE {
			continue
		}
		for _, spec := range generic.Specs {
			specs = append(specs, spec.(*ast.TypeSpec))
		}
	}
	return specs
}
