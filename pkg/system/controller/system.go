package controller

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/system"
)

// RegisterSystem creates the shared control-plane schema and resolves the
// singleton system row, returning it. Idempotent. cfg may be nil or a sparse
// struct -- WithDefaults fills every field left unset, Validate rejects
// what's out of range.
func (c *SystemController) RegisterSystem(ctx context.Context, cfg *SystemConfig) (*system.System, error) {
	if cfg == nil {
		cfg = &SystemConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	registered, err := c.datastore.RegisterSystem(ctx)
	if err != nil {
		return nil, err
	}

	owner, err := common.NewSystemOwner(registered.Id)
	if err != nil {
		return nil, err
	}
	for _, declarer := range c.declarers {
		if err := declarer.Declare(ctx, owner); err != nil {
			return nil, err
		}
	}
	return toSystem(registered), nil
}

// GetSystem returns the singleton system config, or (nil, nil) if the system
// hasn't been registered.
func (c *SystemController) GetSystem(ctx context.Context) (*system.System, error) {
	found, err := c.datastore.GetSystem(ctx)
	if err != nil || found == nil {
		return nil, err
	}
	return toSystem(found), nil
}

// DeleteSystem drops the shared control-plane schema -- every table
// RegisterSystem creates. Callers drop the per-topic tables first; a topic
// still registered when this runs leaves its physical tables orphaned.
func (c *SystemController) DeleteSystem(ctx context.Context) error {
	return c.datastore.DeleteSystem(ctx)
}
