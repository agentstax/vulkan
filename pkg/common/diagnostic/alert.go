package diagnostic

import "fmt"

// DiagnosticAlert is a declared Vulkan-owned alert: the identity and
// metadata every check and rendering surface shares. Severity is plain
// text here because its vocabulary belongs to the alert domain.
type DiagnosticAlert struct {
	Code        string
	Name        string
	Description string // the condition the check detects
	Scope       MetricScope
	Severity    string // "warn"
}

// NewDiagnosticAlert declares an alert and registers its code. The name must
// be unique too -- an alert on the wire carries only its name, and GetAlert
// resolves the declaration by that handle. An alert is always about a
// resource, so the consumer-session scope is refused.
func NewDiagnosticAlert(code string, name string, description string, scope MetricScope, severity string) *DiagnosticAlert {
	if name == "" {
		panic("name must not be empty: " + code)
	}
	if description == "" {
		panic("description must not be empty: " + code)
	}
	if err := scope.Validate(); err != nil {
		panic(err.Error() + ": " + code)
	}
	if scope == MetricScopeConsumerSession {
		panic(fmt.Sprintf("scope must be a resource scope, got %q: %s", scope, code))
	}
	if severity == "" {
		panic("severity must not be empty: " + code)
	}
	if existing, ok := GetAlert(name); ok {
		panic("alert name already registered as " + existing.Code + ": " + name)
	}

	declared := &DiagnosticAlert{
		Code:        code,
		Name:        name,
		Description: description,
		Scope:       scope,
		Severity:    severity,
	}
	register(declared)
	return declared
}

// Docs returns the alert's documentation page, derived from the code.
func (a *DiagnosticAlert) Docs() string {
	return docsBaseURL + a.Code
}

// GetCode and GetKind satisfy Declaration; Get-prefixed because Code is
// already the field.
func (a *DiagnosticAlert) GetCode() string {
	return a.Code
}

func (a *DiagnosticAlert) GetKind() DiagnosticKind {
	return DiagnosticKindAlert
}

// Alerts lists every registered alert ordered by code.
func Alerts() []*DiagnosticAlert {
	return listRegistered[*DiagnosticAlert]()
}

// GetAlert returns the declaration behind an alert name; comma-ok absence
// for names the registry does not know (user alerts on the same topic).
func GetAlert(name string) (*DiagnosticAlert, bool) {
	registryLock.Lock()
	defer registryLock.Unlock()

	for _, registered := range registeredDeclarations {
		if declared, ok := registered.(*DiagnosticAlert); ok && declared.Name == name {
			return declared, true
		}
	}
	return nil, false
}
