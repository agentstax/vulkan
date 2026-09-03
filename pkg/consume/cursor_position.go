package consume

import "fmt"

type CursorPositionKind string

const (
	// CursorPositionBeginning is the zero value: the oldest retained message.
	CursorPositionBeginning CursorPositionKind = ""
	// CursorPositionHead is MAX(id) of the message log when the cursor row is
	// written; a produce with a lower id that commits later is never read.
	CursorPositionHead CursorPositionKind = "head"
)

func (k CursorPositionKind) Validate() error {
	switch k {
	case CursorPositionBeginning, CursorPositionHead:
		return nil
	default:
		return fmt.Errorf("must be one of %q, %q, got %q", CursorPositionBeginning, CursorPositionHead, k)
	}
}

// CursorPosition is a place in a topic's message log a group's cursor is set
// to -- by Register for a group that has no cursor row yet.
type CursorPosition struct {
	Kind CursorPositionKind
}

func Beginning() CursorPosition {
	return CursorPosition{Kind: CursorPositionBeginning}
}

func Head() CursorPosition {
	return CursorPosition{Kind: CursorPositionHead}
}
