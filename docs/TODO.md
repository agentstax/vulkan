# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Cross-version compatibility matrix (14c)

Design settled 2026-08-22; the full shape lives in ROADMAP.md's item and
stays there until ship. Tasks in build order:

1. (done 2026-08-22) Registry declaration: Migration gains
   `MinCompatibleVersion int64` (0 = additive; == Version = breaking for
   everyone older); Validate gains `0 <= MinCompatibleVersion <= Version`;
   the doc comment's authoring rules gain the declaration rule and the
   no-op-bump rule.
2. (done 2026-08-22) migration_log baseline DDL gains
   `min_compatible_version BIGINT NOT NULL DEFAULT 0`; recordSuccess
   writes the step's declaration (baseline-creation and down rows write
   0); verified by dev-DB drop+recreate.
3. (done 2026-08-22) Collapse the four Min/Max build constants to one
   derived version per scope: migrations.Version() beside each Registry
   (`len(Registry) + 1`), migrate/support.go deleted; the gate passes the
   build version as both bounds until task 5's predicate lands.
4. (done 2026-08-22) Two-fact schema-state read: SchemaStateData +
   System/TopicSchemaState pairs replace the single-version datastore
   reads (controller SystemVersion/TopicVersion re-shaped over them, one
   read path); the runner's own Version() read unchanged; exercised live
   via schema-gate-lab.
5. (done 2026-08-22) Gate predicate: assertVersionSupported allows iff
   `min_compatible_version <= buildVersion <= current`; VK0023 attrs now
   min_compatible_version + build_version (runner's raise site aligned);
   both keys added to the CONVENTIONS attr registry; website pages
   unchanged on purpose -- problem/recovery/fix text did not change and
   the uniform page shape carries no attrs (floor semantics land on task
   8's migration docs page).
6. (done 2026-08-22) schemagatelab reshaped: additive-skew acceptance
   (the rolling-deploy window), breaking-step refusal naming
   min_compatible_version, and per-topic skew via a sibling topic (forged
   breaking row on one family, sibling still registers). Older-than stays
   unforgeable while the registries are empty (build version cannot
   exceed the baseline) -- it gets a real assertion with the first real
   migration.
7. (open) tools/compat module skeleton + `just compat-lab` recipe —
   dry-runs via a replace to the same tree until two releases exist.
8. (open) Rules & docs: CONVENTIONS ## Migrations release-era rules +
   tools/ wording widened to "dev-only modules"; website migration docs
   page with the per-release compatibility table; AGENTS.md release
   checklist; decision record.
