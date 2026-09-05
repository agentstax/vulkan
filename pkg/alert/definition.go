package alert

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// AlertDefinition is one Vulkan built-in alert's identity and metadata. It
// exists before any alert is published.
type AlertDefinition struct {
	Code        string                 `json:"code"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Scope       diagnostic.MetricScope `json:"scope"`
	Severity    AlertSeverity          `json:"severity"`
}

// Definitions returns Vulkan's built-in alert definitions ordered by VK code.
// With no scopes it returns the whole catalog; otherwise it returns
// definitions belonging to any requested scope.
func Definitions(scopes ...diagnostic.MetricScope) []AlertDefinition {
	requestedScopes := make(map[diagnostic.MetricScope]bool, len(scopes))
	for _, scope := range scopes {
		requestedScopes[scope] = true
	}

	registered := diagnostic.Alerts()
	definitions := make([]AlertDefinition, 0, len(registered))
	for _, declared := range registered {
		if len(requestedScopes) > 0 && !requestedScopes[declared.Scope] {
			continue
		}
		definitions = append(definitions, AlertDefinition{
			Code:        declared.Code,
			Name:        declared.Name,
			Description: declared.Description,
			Scope:       declared.Scope,
			Severity:    AlertSeverity(declared.Severity),
		})
	}
	return definitions
}
