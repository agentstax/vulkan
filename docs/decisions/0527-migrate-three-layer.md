---
status: accepted
date: 2026-08-17
phase: 14b
---

# pkg/migrate adopts the three-layer template

## Context

After [0526] the migrate door was right but the layout was flat: Controller,
configs, and gate methods sat in the pkg/migrate root beside the vocabulary
(Migration, sentinels, version consts), with the datastore at
pkg/migrate/datastore. Every other domain follows pkg/<x> ->
pkg/<x>/controller -> pkg/<x>/controller/datastore.

## Decision

- pkg/migrate is vocabulary only: Migration + registry Validate,
  ErrNotRegistered, Min/Max version consts. It imports common and core
  datastore (Querier appears in Migration's step-func signatures).
- Controller, ControllerConfig, RunOnce/RunAll, the schema gates, and the
  version/lock reads move to pkg/migrate/controller; the datastore moves to
  pkg/migrate/controller/datastore.
- Migration.ToStep becomes the controller-side toStep adapter -- it builds a
  datastore shape, so it cannot stay on the vocabulary type.
- ErrNotRegistered is declared in the vocabulary (sentinel-ownership rule);
  the datastore's registrationError maps onto it, and the controller's
  re-export var is deleted.

## Consequences

- Callers needing only errors.Is / []Migration keep importing pkg/migrate;
  callers building or holding the door import
  migratecontroller "pkg/migrate/controller" (topic/worker controllers,
  admin, systemmanager, CLI, invariantlab, schemagatelab).
- The later internal/ move ([public surface trim]) relocates this shape
  wholesale; no second restructure needed.
