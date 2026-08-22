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
5. (open) Gate predicate: allowed iff
   `min_compatible_version <= buildVersion <= current`; VK0022/VK0023
   attrs gain min_compatible_version / build_version; their website pages
   update in the same change.
6. (open) schemagatelab reshaped for floor semantics (forge
   min_compatible_version rows for newer-than, a low current for
   older-than) plus the per-topic skew assertion (RunOnce on topic A,
   topic B's family gates independently).
7. (open) tools/compat module skeleton + `just compat-lab` recipe —
   dry-runs via a replace to the same tree until two releases exist.
8. (open) Rules & docs: CONVENTIONS ## Migrations release-era rules +
   tools/ wording widened to "dev-only modules"; website migration docs
   page with the per-release compatibility table; AGENTS.md release
   checklist; decision record.
