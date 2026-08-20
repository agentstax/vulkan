package conventions

// Walks every plain raise string (errors.New / fmt.Errorf string literals)
// under pkg/, otelvulkan/, and cmd/vulkan/ and enforces the mechanical half
// of CONVENTIONS.md "When writing a plain error". Judgment rules (tense,
// name spelling, fix quality) stay review-time. A legitimate exception gets
// an explicit file:line entry here with a reason, never a marker in the code.

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

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

type raiseSite struct {
	Position string // file:line, relative to the repo root
	Call     string // "errors.New" | "fmt.Errorf"
	Message  string
}

func TestPlainRaiseStringsAvoidBannedWords(t *testing.T) {
	for _, site := range plainRaiseSites(t) {
		if match := bannedWords.FindString(site.Message); match != "" {
			t.Errorf("%s contains banned word %q: %q", site.Position, match, site.Message)
		}
		if strings.Contains(site.Message, "!") {
			t.Errorf("%s contains an exclamation point: %q", site.Position, site.Message)
		}
	}
}

func TestPlainStaticMessagesUseErrorsNew(t *testing.T) {
	for _, site := range plainRaiseSites(t) {
		if site.Call != "fmt.Errorf" {
			continue
		}
		if !strings.Contains(strings.ReplaceAll(site.Message, "%%", ""), "%") {
			t.Errorf("%s builds a static message with fmt.Errorf -- use errors.New: %q", site.Position, site.Message)
		}
	}
}

// constraintLead matches the identifier-led constraint template
// (`<name> must be <constraint>`); trailing clauses ("versions must be
// contiguous ...") stay unmatched.
var constraintLead = regexp.MustCompile(`^\S+ must be `)

func TestPlainConstraintGuardsCarryTheValue(t *testing.T) {
	for _, site := range plainRaiseSites(t) {
		if !constraintLead.MatchString(site.Message) {
			continue
		}

		// absence-shaped constraints (nil/zero) have no violating value to show
		if strings.Contains(site.Message, " must be nil") || strings.Contains(site.Message, " must be zero") {
			continue
		}

		if !strings.Contains(site.Message, ", got ") {
			t.Errorf("%s constraint guard lacks the violating value (\", got <value>\"): %q", site.Position, site.Message)
		}
	}
}

func TestPlainRaiseStringsNeverRestateDeclaredProblems(t *testing.T) {
	for _, site := range plainRaiseSites(t) {
		for _, registered := range diagnostic.Errors() {
			if strings.Contains(site.Message, registered.Problem) {
				t.Errorf("%s restates %s -- raise the declared variable instead: %q", site.Position, registered.Code, site.Message)
			}
		}
	}
}

// ***************
// *** HELPERS ***
// ***************

func plainRaiseSites(t *testing.T) []raiseSite {
	t.Helper()
	root := repoRoot(t)

	var sites []raiseSite
	for _, tree := range []string{"pkg", "otelvulkan", "cmd/vulkan"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			// vendored source keeps its upstream wording
			if strings.Contains(path, filepath.Join("cron", "internal", "robfig")) {
				return nil
			}

			fileSet := token.NewFileSet()
			parsed, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				call, message, ok := raiseLiteral(node)
				if !ok {
					return true
				}
				relative, err := filepath.Rel(root, path)
				if err != nil {
					relative = path
				}
				sites = append(sites, raiseSite{
					Position: relative + ":" + strconv.Itoa(fileSet.Position(node.Pos()).Line),
					Call:     call,
					Message:  message,
				})
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return sites
}

// raiseLiteral reports the call name and unquoted message when node is an
// errors.New or fmt.Errorf call on a string literal.
func raiseLiteral(node ast.Node) (string, string, bool) {
	call, ok := node.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", "", false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	receiver, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}

	name := receiver.Name + "." + selector.Sel.Name
	if name != "errors.New" && name != "fmt.Errorf" {
		return "", "", false
	}

	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", "", false
	}
	message, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", "", false
	}
	return name, message, true
}
