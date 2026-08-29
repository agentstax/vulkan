package base

import (
	"errors"

	"github.com/agentstax/vulkan/pkg/common/diagnostic"
	"github.com/agentstax/vulkan/pkg/consumergroup"
)

// HandlerOutcome is what a consumerFunc's returned error asks for.
type HandlerOutcome int

const (
	HandlerOutcomeException HandlerOutcome = iota // retry on the backoff curve
	HandlerOutcomeTerminal                        // dead now -- no retry could succeed
	HandlerOutcomeDelayed                         // run again after the duration the error carries, no failure counted
)

// ClassifyHandlerError reads the outcome off the error's own classification:
// a DelayedDelivery anywhere in the chain -> delayed, a Permanent declaration
// -> terminal, everything else (Transient or plain) -> exception. The wrong
// default here loses a message; the other only costs a backoff curve.
func ClassifyHandlerError(err error) HandlerOutcome {
	if _, ok := errors.AsType[*consumergroup.DelayedDelivery](err); ok {
		return HandlerOutcomeDelayed
	}
	if classified, ok := errors.AsType[*diagnostic.Error](err); ok && classified.Recovery == diagnostic.Permanent {
		return HandlerOutcomeTerminal
	}
	return HandlerOutcomeException
}
