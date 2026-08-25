# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## A diagnose part on diagnostic declarations (picked up 2026-08-25)

Picked up because paste-your-log-line (ROADMAP, Doc site -- interactive
mechanisms) is blocked on it: with nothing declared to look at, that page
can only interpolate the fix string, which is a search box with extra
steps. Today a declaration carries code, recovery, problem, fix (plus
consequence on events) -- the fix says what to CHANGE, nothing says what
to LOOK AT. Vulkan's answer is that the state is rows, so the diagnosis
is SQL.

Four facts from the 2026-08-25 survey that decide the shape:

- The attributes are already at the call sites. VK0029 logs topic_id,
  group_id, message_id; VK0026/VK0027 log topic_id, group_id, low, high.
  A pasted line already carries every value a per-topic query needs.
- Per-topic table names are pure functions of topic_id
  (internal/topic.DeliveryTable), so `delivery_{topic_id}` is a
  placeholder, not a lookup.
- There is no Go-side attribute registry -- the log attribute table is
  prose in CONVENTIONS.md only, so nothing can catch a template
  placeholder that names no real attribute.
- The site's error pages already hand-copy declarations (frontmatter
  title/fix/recovery are duplicates, drift check still parked). Adding
  hand-copied SQL there bets the same way on the content most likely to
  rot.

### Settled (2026-08-25)

- Shape: an ordered small set, chained onto the declaration. Most
  conditions want "is the row there?" then "what does its state say?".

      var EventMessageDeadLettered = diagnostic.NewEvent("VK0029",
          "message dead-lettered", "unrecoverable, will not be retried").
          Diagnose(
              diagnostic.NewQuery("the delivery row's terminal state", `
                  SELECT status, attempts, last_error, updated_at
                  FROM delivery_{topic_id}
                  WHERE consumer_group_id = {group_id} AND message_id = {message_id}`),
              diagnostic.NewQuery("every attempt it made", `
                  SELECT attempt, status, error, attempted_at
                  FROM delivery_log_{topic_id}
                  WHERE consumer_group_id = {group_id} AND message_id = {message_id}
                  ORDER BY attempt`),
          )

  Chained rather than a fifth NewError param or an ErrorConfig: about
  half the declarations have nothing to look at and should not pay a
  nil. It is not functional options -- no closures, no variadic opts,
  just a named method that panics at init the way NewError already does.
- Diagnose is the WithDefaults pattern -- mutates the receiver in
  place and returns it -- NEVER the With/Wrap copy pattern: NewError/
  NewEvent register the pointer at init, and every surface (explain,
  codeexport) reads declarations from the registry, so a copy would
  leave the registered original bare and every surface would silently
  render nothing. Panics on a second call to the same declaration.
- Placeholders are attribute names from the log attribute registry,
  `{attribute_name}`. The declaration's own SQL carries the quoting each
  placeholder's position needs: identifier-position placeholders bare
  (`delivery_{topic_id}`), value-position placeholders quoted per their
  column type (`'{topic}'` for text/uuid, bare `{group_id}` and
  `{message_id}` for bigint). psql-variable placeholders
  (`:'group_id'`) were considered and rejected -- psql vars cannot
  concatenate inside an identifier, so `delivery_{topic_id}` has no
  psql spelling.
- Metrics (the registry's third kind) declare nothing for now -- the
  exclusion is deliberate, not an oversight.
- The site reads the queries as exported data, never hand-copied prose:
  tools/codeexport -> website/src/data/codes.json, regenerate-and-diff
  in `just site-verify`. compat.json [0588] is the precedent. The
  export carries the WHOLE declaration record -- problem, fix,
  recovery, message, consequence, queries -- not queries alone: the
  error pages' frontmatter (title/fix/recovery) is hand-copied today
  with its drift check parked, and the full record makes that check a
  trivial comparison in `just site-verify`. Prose stays hand-written
  (the standing no-generated-docs rule) -- the JSON is verification
  data plus the paste page's templates, never rendered page prose.
- Where the SQL text lives: a declaration-time const beside the Err*/
  Event, not a datastore method. ## SQL puts all SQL in datastores
  because datastores EXECUTE it; the library never runs this SQL.
- `vulkan explain --run` stays unbuilt. Placeholders named by
  attribute key keep it possible later (the CLI takes --topic-id-style
  flags) without a redesign.
- The CLI error block stays tight -- it points at `vulkan explain`.

### Open

- [x] How a typo'd placeholder gets caught -- SETTLED: the
      tools/conventions test below. It lands with task 3, when there are
      declared queries to check.
      - Recommended: a tools/conventions test that parses the
        ### Attributes table out of CONVENTIONS.md and validates every
        declared query's placeholders -- prose stays the single source,
        zero production scope, fails at `just verify` like every other
        machine-checkable rule. The TS paste parser does NOT need the
        attribute list (it parses key=value pairs from the line and fills
        placeholders by name from the templates already in codes.json),
        so nothing else needs the list either.
      - The log attribute registry as real Go code (~30 names in
        diagnostic), NewQuery panicking on an unknown placeholder.
        Only worth it if the registry earns more than this feature --
        e.g. later enforcing that log call sites use registered keys.
      - No check and accepted drift.

### Tasks

- [x] 1. The proposal page: VK0029's thread page with its diagnose
      section rendered, reviewed before any Go lands (docs drive
      implementation). Decides how a reader meets the section -- heading,
      placeholder rendering before values are known, and whether the two
      queries read as one block or two.
- [x] 2. diagnostic: Query + NewQuery, Diagnose on Error and Event,
      placeholder parsing. Panics at init on a malformed template, same
      as the other declaration-time guards.
- [x] 3. Declare the queries. Rough coverage from the survey: dead-letter
      and lease events (VK0026-31), worker events (VK0034-36), the schema
      gate (VK0022/23), not-found and destroy guards (VK0005/06/14/15/16/
      20), commit-lost (VK0019). Constructor and config guards get
      nothing and no section renders -- absence is honest.
- [x] 4. tools/codeexport (full declaration record, per the Settled
      export bullet) + the site-verify drift guard, including the
      previously parked frontmatter check. Before the CLI tasks:
      this plus task 5 is what unblocks paste-your-log-line.
- [ ] 5. The code thread pages render the section from codes.json.
- [ ] 6. `vulkan explain VKxxxx` renders the section; --output json
      carries it.
- [ ] 7. Doc comments add nothing: pkg.go.dev renders the var's full
      declaration source, Diagnose call and SQL included, so a copied
      query in the comment is the hand-copy drift bet again. At most a
      one-line pointer ("diagnose queries: vulkan explain VK0029") on
      declarations that have one.
- [ ] 8. Decision record + HISTORY entry; ROADMAP's diagnose item slims
      to a pointer and paste-your-log-line unblocks.
