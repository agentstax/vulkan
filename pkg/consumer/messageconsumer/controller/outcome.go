package controller

import "fmt"

// MessageOutcome is one resolved message of a claimed range, written by
// Commit or PartialCommit.
type MessageOutcome struct {
	MessageId int64
	Kind      OutcomeKind
	Err       string
}

// OutcomeKind is how one message of a claimed range resolved.
type OutcomeKind string

const (
	OutcomeException  OutcomeKind = "exception"  // retryable -- writes a 'ready' delivery row instead of failing the whole range
	OutcomeTerminal   OutcomeKind = "terminal"   // no retry could ever succeed -- writes the delivery row straight to 'dead'
	OutcomeSuperseded OutcomeKind = "superseded" // a newer message on its compaction key exists -- log row only, never a delivery row
	OutcomeDeferred   OutcomeKind = "deferred"   // another delivery held its key -- writes a 'deferred' delivery row for the exception window
	OutcomeSuccess    OutcomeKind = "success"    // ran clean -- log row only, never a delivery row; callers include it only under DeliveryLogModeAll
)

func (k OutcomeKind) Validate() error {
	switch k {
	case OutcomeException, OutcomeTerminal, OutcomeSuperseded, OutcomeDeferred, OutcomeSuccess:
		return nil
	}
	return fmt.Errorf("must be one of %q, %q, %q, %q, %q, got %q", OutcomeException, OutcomeTerminal, OutcomeSuperseded, OutcomeDeferred, OutcomeSuccess, k)
}
