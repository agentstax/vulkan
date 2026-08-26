package diagnostic

// The {attribute_name} vocabulary a declaration's diagnose queries and its fix
// share. Both name log attributes, so the condition's own line carries what
// fills them.

import (
	"log/slog"
	"regexp"
	"slices"
	"strings"
)

var placeholderPattern = regexp.MustCompile(`\{[a-z][a-z0-9_]*\}`)

func placeholderNames(text string) []string {
	found := placeholderPattern.FindAllString(text, -1)

	names := make([]string, 0, len(found))
	for _, placeholder := range found {
		name := strings.Trim(placeholder, "{}")
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// The value goes in raw: the text around a placeholder carries whatever
// quoting its position needs.
func fillPlaceholders(text string, values []slog.Attr) string {
	return placeholderPattern.ReplaceAllStringFunc(text, func(placeholder string) string {
		name := strings.Trim(placeholder, "{}")
		for _, attribute := range values {
			if attribute.Key == name {
				return attribute.Value.String()
			}
		}
		return placeholder
	})
}
