package diagnostic

// Event is a declared operator-actionable log event: the static message
// a call site logs and the code that rides in its "code" attr.
type Event struct {
	Code    string
	Message string
}

// NewEvent declares a log event and registers its code. A non-empty
// consequence is appended to the message after " -- ".
func NewEvent(code string, message string, consequence string) *Event {
	if message == "" {
		panic("message must not be empty: " + code)
	}

	if consequence != "" {
		message = message + " -- " + consequence
	}

	declared := &Event{Code: code, Message: message}
	register(declared)
	return declared
}

// Docs returns the event's documentation page, derived from the code.
func (e *Event) Docs() string {
	return docsBaseURL + e.Code
}

// GetCode and GetKind satisfy Declaration; Get-prefixed because Code is
// already the field.
func (e *Event) GetCode() string {
	return e.Code
}

func (e *Event) GetKind() Kind {
	return KindEvent
}

// Events lists every registered log event ordered by code.
func Events() []*Event {
	return listRegistered[*Event]()
}
