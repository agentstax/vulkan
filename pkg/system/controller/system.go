package controller

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/system"
)

// Register creates the shared control-plane schema and resolves the
// singleton system row, returning it. Idempotent. cfg may be nil or a sparse
// struct -- WithDefaults fills every field left unset, Validate rejects
// what's out of range.
func (c *SystemController) Register(ctx context.Context, cfg *SystemConfig) (*system.SystemData, error) {
	if cfg == nil {
		cfg = &SystemConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	registered, err := c.datastore.Register(ctx)
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
	return toSystemData(registered), nil
}

// Get returns the singleton system config, or (nil, nil) if the system
// hasn't been registered.
func (c *SystemController) Get(ctx context.Context) (*system.SystemData, error) {
	found, err := c.datastore.Get(ctx)
	if err != nil || found == nil {
		return nil, err
	}
	return toSystemData(found), nil
}

// Delete drops the shared control-plane schema -- every table
// Register creates. Callers drop the per-topic tables first; a topic
// still registered when this runs leaves its physical tables orphaned.
func (c *SystemController) Delete(ctx context.Context) error {
	return c.datastore.Delete(ctx)
}
