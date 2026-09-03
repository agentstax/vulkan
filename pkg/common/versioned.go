package common

// Versioned is what every Message type declares: the payload's
// compatibility version, written on every message row it produces. A
// value receiver, so the zero value answers.
//
// Bump only on a BREAKING change to Message:
// - a field has a different type
// - a field has been renamed
// - a field has been removed
//
// A consumer group reads only rows at its Message type's version.
type Versioned interface {
	SchemaVersion() int
}

// SchemaVersionOf reads the version a Message type declares.
func SchemaVersionOf[Message Versioned]() int {
	var message Message
	return message.SchemaVersion()
}
