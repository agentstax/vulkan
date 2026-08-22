---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# 0580 — Every migration step declares MinCompatibleVersion; the schema gate admits min_compatible_version <= build <= current

**Context.** One stored number served two facts — what version a scope's
schema is at, and who may run against it. The gate refused any stored
version above the build's Max constant, so ANY migration locked every older
binary out at Register, additive or not: rolling deploys were impossible by
construction. Industry survey converged on "minimum compatible version":
HDFS minCompatibleLayoutVersion (declared per feature; a fully breaking
feature sets it to itself — structurally identical), Delta Lake
minReaderVersion, Elasticsearch minimum_index_compatibility_version,
CockroachDB binaryMinSupportedVersion.

**Decision.** Each migrate.Migration declares `MinCompatibleVersion int64`
— the oldest build schema version whose SQL still runs against the schema
the step produces; 0 = additive, the step's own version = breaking,
validated to [0, Version]. A bool marker was rejected: expand/contract
column removal has a middle release whose binaries tolerate the drop while
older ones don't — only a number can name it. The companion authoring rule:
a release that changes only what binaries read ships an empty version-bump
step, so later steps have a version to name.

recordSuccess stores the declaration per row
(`migration_log.min_compatible_version`, in the v1.0 baseline DDL — v1.0
binaries cannot retrofit a column they don't know). The gate reads both
facts per scope in one query — successes, current (latest-by-id, [0343]),
compatibility = MAX(min_compatible_version) among steps at or below current
— so a downgrade unbinds rolled-back steps with no special handling, and
admits a build iff `min_compatible_version <= buildVersion <= current`.
The build version is derived beside each registry (`len(Registry) + 1`);
the four Min/Max constants were deleted.

The empirical check is tools/compat: a nested module (own go.mod + nested
go.work) pinning the prior release, driving ONLY the public API against a
working-tree-migrated database, asserting the registry's declared verdict
— `just compat-lab`, release checkpoints only. The replace points at the
working tree until two releases exist.

**Consequences.** Additive migrations keep N-1 binaries registering and
running through a deploy (VK0023 fires only past a breaking step; attrs now
min_compatible_version + build_version). Breaking releases are procedural —
stop old binaries first — because the gate runs only at Register; live
processes fail on their own SQL, as in every system surveyed. VK0022
older-than cannot be exercised in labs until the first real migration
exists (the build version cannot exceed the baseline). schemagatelab
asserts the additive window, the breaking refusal, and per-topic skew; the
per-release compatibility table lives in the migrations docs page, kept
honest by the compat lab and the AGENTS.md release checklist.
