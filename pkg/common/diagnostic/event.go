package diagnostic

// Event is a declared operator-actionable log event: the static message
// a call site logs and the code that rides in its "code" attribute.
type DiagnosticEvent struct {
	Code    string
	Message string
	Queries []*DiagnosticQuery // none when the event has no state to look at
}

// NewDiagnosticEvent declares a log event and registers its code. A non-empty
// consequence is appended to the message after " -- ".
func NewDiagnosticEvent(code string, message string, consequence string) *DiagnosticEvent {
	if message == "" {
		panic("message must not be empty: " + code)
	}

	if consequence != "" {
		message = message + " -- " + consequence
	}

	declared := &DiagnosticEvent{Code: code, Message: message}
	register(declared)
	return declared
}

// Diagnose attaches the queries that show an operator the state behind this
// event, and returns the same declaration so it chains onto NewDiagnosticEvent.
func (e *DiagnosticEvent) Diagnose(queries ...*DiagnosticQuery) *DiagnosticEvent {
	if len(queries) == 0 {
		panic("diagnose queries must not be empty: " + e.Code)
	}
	if len(e.Queries) > 0 {
		panic("diagnose queries are already declared: " + e.Code)
	}

	e.Queries = queries
	return e
}

// Docs returns the event's documentation page, derived from the code.
func (e *DiagnosticEvent) Docs() string {
	return docsBaseURL + e.Code
}

// GetCode and GetKind satisfy Declaration; Get-prefixed because Code is
// already the field.
func (e *DiagnosticEvent) GetCode() string {
	return e.Code
}

func (e *DiagnosticEvent) GetKind() DiagnosticKind {
	return DiagnosticKindEvent
}

// Events lists every registered log event ordered by code.
func Events() []*DiagnosticEvent {
	return listRegistered[*DiagnosticEvent]()
}
