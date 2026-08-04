package controller

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/system"
)

// RegisterSystem creates the shared control-plane schema and resolves the
// singleton system config, returning it. Idempotent -- a cfg matching the
// seeded row resolves as a no-op; a differing one errors with
// system.ErrSystemConfigMismatch. cfg may be nil or a sparse struct --
// WithDefaults fills every field left unset, Validate rejects what's out of
// range.
func (c *SystemController) RegisterSystem(ctx context.Context, cfg *SystemConfig) (*system.System, error) {
	if cfg == nil {
		cfg = &SystemConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	registered, err := c.datastore.RegisterSystem(ctx, toRegisterSystemData(cfg))
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

// UpdateSystem applies cfg's non-nil fields to the singleton system config and
// returns the updated config. Returns (nil, nil) if the system hasn't been
// registered.
func (c *SystemController) UpdateSystem(ctx context.Context, cfg *AlterSystemConfig) (*system.System, error) {
	if cfg == nil {
		cfg = &AlterSystemConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	updated, err := c.datastore.UpdateSystem(ctx, toAlterSystemData(cfg))
	if err != nil || updated == nil {
		return nil, err
	}
	return toSystem(updated), nil
}
