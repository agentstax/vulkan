package conventions

// vulkan is the client plus aliases (CONVENTIONS.md ## Package layout): one
// import spells every type, const, declared error, and declared event a
// user meets. The set is computed, never hand-kept -- a go/types walk over
// pkg/vulkan's exported surface finds every declaration from this module a
// caller can reach, and each one must have a vulkan alias or var whose
// target is that very object, under the same name. Two roots declaring
// `Kind` cannot both be spelled `vulkan.Kind`, so the check compares
// targets, never bare names.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const vulkanPath = modulePath + "/pkg/vulkan"

func TestVulkanCoversEveryReachableDeclaration(t *testing.T) {
	closure := vulkanClosure(t)

	for _, reached := range closure.reachable() {
		spelled, ok := closure.provided[reached]
		switch {
		case !ok:
			t.Errorf("%s.%s is reachable from vulkan but vulkan declares no alias or var for it", reached.Pkg().Path(), reached.Name())
		case spelled != reached.Name():
			t.Errorf("%s.%s is spelled vulkan.%s -- an alias keeps the declaration's name", reached.Pkg().Path(), reached.Name(), spelled)
		}
	}
}

// The machinery floor: a controller, datastore, batcher, or worker package
// declares nothing a user spells except its own Config and *Row structs and
// its controller / datastore / instance / provisioner types. What a user
// spells is exactly what vulkan reaches, so the check runs over the same
// closure: every reachable object declared below a root must carry one of
// those suffixes.
func TestMachineryDeclaresNothingUserSpelled(t *testing.T) {
	closure := vulkanClosure(t)

	for _, reached := range closure.reachable() {
		if !isMachinery(reached.Pkg().Path()) {
			continue
		}
		if !hasAnySuffix(reached.Name(), "Config", "Row", "Controller", "Datastore", "Instance", "Provisioner") {
			t.Errorf("%s.%s is reachable from vulkan but declared in machinery -- move it to common, the root, or the assembler", reached.Pkg().Path(), reached.Name())
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

// exportListing is the go list -json field the walk reads.
type exportListing struct {
	ImportPath string
	Export     string
}

// closure is what one walk of pkg/vulkan found: every object from this
// module a caller can reach through its exported surface, and the objects
// vulkan's own aliases, consts, and vars point at.
type closure struct {
	imp      types.Importer
	seen     map[*types.Named]bool
	reached  map[types.Object]bool
	provided map[types.Object]string
}

// vulkanClosure type-checks pkg/vulkan from source over the export data of
// its dependencies, then walks its exported scope. A package reached only
// through another's export data holds the objects that data references and
// nothing more, so each one is imported directly before its consts are read.
func vulkanClosure(t *testing.T) *closure {
	t.Helper()
	root := repoRoot(t)

	list := exec.Command("go", "list", "-export", "-deps", "-json", vulkanPath)
	list.Dir = root
	list.Stderr = os.Stderr
	listed, err := list.Output()
	if err != nil {
		t.Fatal(err)
	}
	exports := map[string]string{}
	var dependencies []string
	decoder := json.NewDecoder(bytes.NewReader(listed))
	for {
		var entry exportListing
		if err := decoder.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if entry.Export != "" {
			exports[entry.ImportPath] = entry.Export
		}
		if strings.HasPrefix(entry.ImportPath, modulePath+"/") && entry.ImportPath != vulkanPath {
			dependencies = append(dependencies, entry.ImportPath)
		}
	}

	fileSet := token.NewFileSet()
	imp := importer.ForCompiler(fileSet, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	})

	paths, err := filepath.Glob(filepath.Join(root, "pkg", "vulkan", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, parsed)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	config := types.Config{Importer: imp}
	vulkan, err := config.Check(vulkanPath, fileSet, files, info)
	if err != nil {
		t.Fatal(err)
	}

	c := &closure{imp: imp, seen: map[*types.Named]bool{}, reached: map[types.Object]bool{}, provided: map[types.Object]string{}}
	scope := vulkan.Scope()
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		if !object.Exported() {
			continue
		}
		c.walkType(object.Type())
	}
	for _, path := range dependencies {
		c.reachVars(t, path)
	}
	for _, file := range files {
		c.readProvided(file, info)
	}
	return c
}

// reachable lists the found objects in a stable order.
func (c *closure) reachable() []types.Object {
	objects := make([]types.Object, 0, len(c.reached))
	for object := range c.reached {
		objects = append(objects, object)
	}
	sort.Slice(objects, func(i int, j int) bool {
		left := objects[i].Pkg().Path() + "." + objects[i].Name()
		right := objects[j].Pkg().Path() + "." + objects[j].Name()
		return left < right
	})
	return objects
}

// walkType follows one type to every named type it mentions: element
// types, exported fields, method signatures, type parameters and their
// constraints.
func (c *closure) walkType(t types.Type) {
	switch t := t.(type) {
	case *types.Named:
		c.walkNamed(t)
	case *types.Alias:
		c.walkType(types.Unalias(t))
	case *types.Pointer:
		c.walkType(t.Elem())
	case *types.Slice:
		c.walkType(t.Elem())
	case *types.Array:
		c.walkType(t.Elem())
	case *types.Map:
		c.walkType(t.Key())
		c.walkType(t.Elem())
	case *types.Chan:
		c.walkType(t.Elem())
	case *types.Signature:
		c.walkTuple(t.Params())
		c.walkTuple(t.Results())
		c.walkTypeParams(t.TypeParams())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if t.Field(i).Exported() {
				c.walkType(t.Field(i).Type())
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			c.walkType(t.Method(i).Type())
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			c.walkType(t.EmbeddedType(i))
		}
	case *types.TypeParam:
		c.walkType(t.Constraint())
	}
}

func (c *closure) walkTuple(t *types.Tuple) {
	for i := 0; i < t.Len(); i++ {
		c.walkType(t.At(i).Type())
	}
}

func (c *closure) walkTypeParams(t *types.TypeParamList) {
	for i := 0; i < t.Len(); i++ {
		c.walkType(t.At(i).Constraint())
	}
}

// walkNamed records a named type from this module and follows it inward.
// A type from outside the module is followed only through its type
// arguments -- nothing outside the module can mention a module type
// otherwise.
func (c *closure) walkNamed(t *types.Named) {
	if c.seen[t] {
		return
	}
	c.seen[t] = true
	for i := 0; i < t.TypeArgs().Len(); i++ {
		c.walkType(t.TypeArgs().At(i))
	}

	object := t.Obj()
	if object.Pkg() == nil || !strings.HasPrefix(object.Pkg().Path(), modulePath+"/") {
		return
	}
	if object.Pkg().Path() != vulkanPath && object.Exported() {
		c.reached[object] = true
		c.reachConsts(object)
	}
	c.walkTypeParams(t.TypeParams())
	c.walkType(t.Underlying())
	for i := 0; i < t.NumMethods(); i++ {
		if t.Method(i).Exported() {
			c.walkType(t.Method(i).Type())
		}
	}
}

// reachConsts records every exported const of a reached type, read from the
// type's own package imported directly.
func (c *closure) reachConsts(typeName *types.TypeName) {
	full, err := c.imp.Import(typeName.Pkg().Path())
	if err != nil {
		panic(err)
	}
	scope := full.Scope()
	for _, name := range scope.Names() {
		constant, ok := scope.Lookup(name).(*types.Const)
		if !ok || !constant.Exported() {
			continue
		}
		if named, ok := types.Unalias(constant.Type()).(*types.Named); ok && named.Obj() == typeName {
			c.reached[constant] = true
		}
	}
}

// reachVars records every exported Err* and Event* var of one package
// vulkan links: a declared error or event is reachable the moment its
// package is, whether or not a signature mentions it.
func (c *closure) reachVars(t *testing.T, path string) {
	t.Helper()
	full, err := c.imp.Import(path)
	if err != nil {
		t.Fatal(err)
	}
	scope := full.Scope()
	for _, name := range scope.Names() {
		variable, ok := scope.Lookup(name).(*types.Var)
		if !ok || !variable.Exported() {
			continue
		}
		if strings.HasPrefix(name, "Err") || strings.HasPrefix(name, "Event") {
			c.reached[variable] = true
		}
	}
}

// readProvided records, for every alias, const, and var in one vulkan file
// whose right-hand side is a qualified name, the object that name resolves
// to.
func (c *closure) readProvided(file *ast.File, info *types.Info) {
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range generic.Specs {
			switch spec := spec.(type) {
			case *ast.TypeSpec:
				if !spec.Assign.IsValid() {
					continue
				}
				if target := qualifiedTarget(spec.Type, info); target != nil {
					c.provided[target] = spec.Name.Name
				}
			case *ast.ValueSpec:
				for i, name := range spec.Names {
					if i >= len(spec.Values) {
						break
					}
					if target := qualifiedTarget(spec.Values[i], info); target != nil {
						c.provided[target] = name.Name
					}
				}
			}
		}
	}
}

// qualifiedTarget resolves the first pkg.Name selector inside an
// expression -- the alias target, or the instantiated generic behind an
// index expression.
func qualifiedTarget(expression ast.Expr, info *types.Info) types.Object {
	var target types.Object
	ast.Inspect(expression, func(node ast.Node) bool {
		if target != nil {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		qualifier, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		if _, ok := info.Uses[qualifier].(*types.PkgName); !ok {
			return true
		}
		target = info.Uses[selector.Sel]
		return false
	})
	return target
}

// isMachinery reports whether an import path sits below a root: anything
// deeper than pkg/<x> except the common subpackages.
func isMachinery(path string) bool {
	relative := strings.TrimPrefix(path, modulePath+"/")
	if strings.HasPrefix(relative, "pkg/common/") {
		return false
	}
	return strings.Count(relative, "/") >= 2
}

func hasAnySuffix(name string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
