# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Alerts as first-class handles

Give alerts the shape metrics got in [0647][0648]: one declaration catalog,
`Definitions()` per scope, typed selectors, and a handle whose `Latest` and
`History` return bare values. Replaces `SystemHandle.Alerts(ctx)` and
`SystemHandle.Alert(messageKey)`, whose argument is the internal
`<name>/<owner-kind>/<owner-id>` key no caller can compose.

### Settled contract

- Three no-I/O scope handles: `System().Alerts()`, `Topic(...).Alerts()`,
  `Topic(...).Group(...).Alerts()`. Each has `Definitions()` (its scope's
  built-ins, ordered by VK code, no I/O), `Latest(ctx)` (the newest retained
  alert per (name, owner) in the scope, ordered by message key), typed
  selectors for its built-ins, and the string form `Alert(name)`.
- `Alert(name)` lives on every scope, not only the system handle. Every alert,
  built-in or user-produced, has an owner, so a name on a topic handle has
  exact topic semantics; the tree binds the owner. Typed selectors are sugar
  over it: `Topic().Alerts().PartitionCount()`,
  `Group().Alerts().WorkerLiveness()`, `System().Alerts().CompactionReadCost()`.
- One `AlertHandle` shape holds the alert name plus the owner's names and no
  row. `Latest(ctx)` returns the current alert, active or resolved, or
  `(nil, nil)` before its first retained message. `History(ctx, limit)`
  returns retained alerts newest first and rejects a non-positive limit.
- The message key stays `<name>/<owner-kind>/<owner-id>` and `common.Owner`
  is unchanged. Ids are the resource's stable identity: an alert follows a
  topic through a rename and does not merge across destroy-and-recreate.
  Each verb resolves the owner's names to ids when called, the way
  `Group.Get` does, then hands them to the one composer `alert.MessageKey`.
  A missing owner surfaces as the existing not-found error
  (`ErrTopicNotFound`, `ErrGroupNotFound`, `ErrNotRegistered`), never as
  `(nil, nil)`: the checks list owners from the catalog, so a destroyed
  owner's head is dead data and not-found is the truthful answer.
- Reads return bare `*Alert`. `Alert` gains `At time.Time` (`json:"at"`),
  the observation time, the sibling of `Measurement.At`; `NewAlert` takes it
  and rejects zero. No code goes on the wire; the name resolves the
  declaration.
- Built-in alerts are declared once, in the diagnostic registry, as the
  fourth kind beside errors, events, and metrics. A declaration holds Code,
  Name, Description (the condition the check detects), Scope, and Severity.
  Severity is fixed per built-in today (all Warn), so it belongs to the
  declaration and the check reads it from there. Message, Detail, Hint, and
  Data vary per finding and stay on the value.
- Scope reuses `diagnostic.MetricScope` (system, topic, consumer_group are
  three of its four values). Sharing it means the type's name wants to become
  `diagnostic.Scope`; the rename is recorded in this work's decision record
  and lands here if the diff stays small, else it is a ROADMAP Later line.
- Topic and group `Latest` read the system-wide head list once and keep the
  heads whose Owner matches the resolved id, inline in the verb. No
  per-owner query: heads are bounded by resources times three built-ins and
  a dedicated read would be a second read path.

### 1. Write and review the public proposal — review pending

- In `website/src/content/docs/guides/client.mdx`: delete the
  `sys.Alert("partition_count:orders")` line from the current-API block (it
  matches no real key), keep `sys.Alerts` out of the plural-noun sentence,
  and add a **Proposed** aside "Alerts as resources" beside the metrics one
  with the full surface:

  ```go
  alerts, err  := client.System().Alerts().Latest(ctx)                                   // []*vulkan.Alert
  definitions  := client.System().Alerts().Definitions()                                // []vulkan.AlertDefinition; no I/O
  topic        := client.Topic[Order]("orders")
  current, err := topic.Alerts().PartitionCount().Latest(ctx)                            // *vulkan.Alert or nil
  history, err := topic.Group("charge-cards").Alerts().WorkerLiveness().History(ctx, 20) // newest first
  custom, err  := topic.Alerts().Alert("disk_pressure").Latest(ctx)                      // user-produced
  ```

- Show the `AlertDefinition` fields, the scope table (system: none; topic:
  the topic; consumer_group: topic and group), absence behavior, ordering,
  `Alert.At`, and the one difference from measurements stated plainly: an
  alert read resolves its owner first, so a destroyed topic's alert returns
  `ErrTopicNotFound` where a measurement would still read.
- Update the migration table row `ListAlerts` -> `client.System().Alerts().Latest`.
- Review with the user before changing the public API. After the surface
  settles, write decision record 0649 (alerts share the metrics shape and
  the one registry; id key kept; `At` on the wire; `Alert(name)` on every
  scope; the `MetricScope` -> `Scope` rename).

### 2. Declare the built-in alerts in the registry

- Add `diagnostic.DiagnosticAlert` in `pkg/common/diagnostic/alert.go`:
  `NewDiagnosticAlert(code, name, description, scope, severity)` registers
  the code in the shared VK serial space and enforces a unique name;
  `Alerts()` lists by code; `GetAlert(name)` resolves a wire name.
- Add `pkg/alert/alerts.go` holding the three declarations, VK0094-VK0096
  (next after VK0093): `AlertPartitionCount` (topic), `AlertCompactionReadCost`
  (system), `AlertWorkerLiveness` (consumer_group), all severity warn. The
  name consts leave the three check controllers (`AlertPartitionCount` in
  `partitioncount/controller` etc.); the check packages and their `JobName`
  consts read the declaration's Name instead. Machinery declares nothing a
  user spells.
- Add `pkg/alert/definition.go`: `AlertDefinition{Code, Name, Description,
  Scope, Severity}` with json tags, and `Definitions(scopes ...)` as the
  defensive view over `diagnostic.Alerts()`, the twin of
  `metrics.Definitions`.
- Extend CONVENTIONS ## Package layout: every code initializes an exported
  var in the root's `errors.go`, `events.go`, `metrics.go`, or `alerts.go`.
  Link the new kind everywhere the other three are linked: `vulkan explain`
  (list and lookup), `tools/codeexport`, `tools/conventions` declared-code
  walks.
- Land the three hand-written docs pages under
  `website/src/content/docs/errors/` in the same change, headed by the
  declaration's description.

### 3. Put the time on the wire and build from the declaration

- `Alert.At time.Time` with `json:"at"`; `NewAlert(name, owner, status,
  severity, message, at, options)` rejects a zero `at`. Update the six
  callers: the three check `alert.go` builders and the resolve path in
  `pkg/alert/controller`, each passing the run's observation time.
- Each check builds its alert from its declaration: name and severity come
  from `alert.AlertPartitionCount` etc.; the call site supplies owner,
  status, message, at, and options. `NewAlert` keeps the string name so a
  user-produced alert needs no declaration.
- Add the coverage test: every declaration appears through exactly one typed
  selector on its scope's handle, and every selector resolves the
  declaration it claims (the metrics test's twin).

### 4. Build the handle tree

- Replace `pkg/vulkan/alert.go`: `SystemAlertsHandle`, `TopicAlertsHandle`,
  `GroupAlertsHandle` (one file each, following `system_metrics.go` /
  `topic_metrics.go` / `group_metrics.go`), and one `AlertHandle` in
  `alert.go` with `Latest`, `History`, and a private `messageKey(ctx)` that
  resolves the owner through `admin.GetSystem` / `GetTopic` / `GetGroup`
  (`GetGroup`'s `(nil, nil)` becomes `ErrGroupNotFound` here) and calls
  `alert.MessageKey`.
- `Latest` reads the head through the existing
  `Topic[Alert](alert.TopicName).Key(key).CompactionHead` and maps
  `ErrCompactionHeadNotFound` to `(nil, nil)`; `History` reads
  `Key(key).Messages`. Both unwrap the envelope at the Vulkan boundary; the
  metrics unwrap helper becomes one generic unwrap over
  `StoredMessage[Message]` shared by both, or, if that reads worse in place,
  a bare-value read on the key handle -- decide on the real diff.
- Delete `SystemHandle.Alerts(ctx)`, `SystemHandle.Alert(messageKey)`,
  `AlertHandle.Get` / `Messages`, and every `MessageKey()` accessor
  (`AlertHandle`, `KeyHandle`) -- no caller, no sibling handle exposes its
  identity. `admin.ListAlerts` stays as internal adaptation.
- Alias `AlertDefinition` and any new user-spelled names through `pkg/vulkan`
  per [0643]; the closure test in tools/conventions computes the set.
- Update callers: `cmd/vulkan/internal/cli/alert_list.go`,
  `examples/playground/13-alert-consumer`, `examples/phase_1/workerlivenesslab`.

### 5. CLI

- `vulkan alert list [--topic <name> [--group <name>]]` reads the scope's
  `Latest`; the table gains `at`. `--quiet` keeps printing name and owner.
- New `vulkan alert get <name> [--topic <name> [--group <name>]] [--limit N]`:
  without `--limit` prints `Latest` as the one block `alert list` prints per
  row (or "no alert published"); with `--limit` prints `History`, one block
  per retained message. `--group` without `--topic` is a usage error.
- `vulkan explain VK0094` renders the alert declaration like a metric's.

### 6. Verify

- `go test -race` on `pkg/common/diagnostic`, `pkg/alert/...`, `pkg/admin`,
  `pkg/vulkan`, `cmd/vulkan`, `tools/...`; `just verify`.
- Directly affected labs: the worker-liveness lab, playground 13, and the
  alert CLI tests. Full fresh-DB suite at the review-ready checkpoint only.

### 7. Close the work

- Remove the **Proposed** label once implementation and verification match
  the reviewed guide exactly.
- HISTORY.md entry citing [0649]; delete this section and the ROADMAP Now
  line; `docs/THOUGHTS.md` keeps its own list.
