package conventions

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

func TestProblemTenseFollowsRecovery(t *testing.T) {
	for _, registered := range diagnostic.Errors() {
		startsCouldNot := strings.HasPrefix(registered.Problem, "could not ")

		switch registered.Recovery {
		case diagnostic.Transient:
			if !startsCouldNot {
				t.Errorf(`%s is Transient but its problem does not start "could not ": %q`, registered.Code, registered.Problem)
			}
		case diagnostic.Permanent:
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

// TestRegistryCoversEverySourceCode proves the import list above is complete:
// every code declared anywhere under pkg/ must be visible in the registry
// this binary sees, so the walks above miss nothing.
func TestRegistryCoversEverySourceCode(t *testing.T) {
	declared := regexp.MustCompile(`New(?:Error|Event)\("(VK\d{4})"`)

	registered := map[string]bool{}
	for _, entry := range diagnostic.Errors() {
		registered[entry.Code] = true
	}
	for _, entry := range diagnostic.Events() {
		registered[entry.Code] = true
	}

	root := repoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range declared.FindAllStringSubmatch(string(source), -1) {
			if !registered[match[1]] {
				t.Errorf("%s declares %s but the registry misses it -- add its package to conventions.go", path, match[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
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
