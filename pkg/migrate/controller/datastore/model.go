package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/datastore"
)

type StepType string

const (
	StepUp   StepType = "UP"
	StepDown StepType = "DOWN"
)

type Step struct {
	Version              int64
	MinCompatibleVersion int64
	Validate             func(context.Context, datastore.Querier, int64) error
	Apply                func(context.Context, datastore.Querier, int64) error
	NoTxn                bool
}
