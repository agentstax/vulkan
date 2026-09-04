---
status: accepted
date: 2026-09-03
phase: "pre-v1"
---

# 0645 — Binding is a handle under the group; client lists are bare plurals

**Context.** `SystemHandle.ListBindingDeclarations` was the one binding
read: a fleet-wide listing with no way to ask for one group's effective
set. Every binding_config and binding_config_log row is keyed by
consumer_group_id inside one topic's tables, so the resource's identity is
(topic, group), the same as the group's. The client layer had also drifted
into two list spellings: bare plurals beside their singular constructors
(`Topics`/`Topic`, `Groups`/`Group`, `Alerts`/`Alert`) and `List*` on four
leaf reads (`ListWorkers`, `ListMessages`, `ListKeyMessages`,
`ListBindingDeclarations`).

**Decision.** The read-model is `consume.Binding` (was
`BindingDeclaration`), the handle `BindingHandle`, reached as
`client.Topic(t).Group(g).Binding()` with no arguments -- the group is the
identity, as `client.System()` is nameless. `Get` is the comma-ok read:
nil when the topic or group is absent, or when the group never installed a
set and reads the whole topic. The fleet listing stays on the system as
`System().Bindings(ctx)`. Underneath: `MessageAdmin.GetBinding` resolves
names like `GetGroup`; `ConsumeController.GetBinding` picks the newest
installed row from the datastore's per-group `ListGroupBindingConfigLog`,
a Wrap pair over the query the declare transaction already ran. Only `Get`
ships; `Waiting`, `Log`, and `Matches` sit in ROADMAP Later, and
`Declare`/`Clear` stay out per [0511].

Client lists are bare plurals, never `List*`: `Workers`, `Messages`,
`KeyMessages` rename in the same change. `List<Noun>s`/`Get<Noun>` remain
the admin, controller, and datastore spelling.

The `<Noun>Declaration` suffix row leaves CONVENTIONS ## Naming and the
[0644] role list: `BindingDeclaration` was its only user, and the value a
caller reads is the binding itself. The datastore's log-row names keep the
declaration word (`BindingConfigLogRow`, `DeclareBindings`,
`NewestInstalledDeclaration`), which is what those rows are. The stale
`BindingLog*` datastore names became `BindingConfigLog*`, matching the
table since [0611].

The CLI nests the read under the group like `group config`:
`vulkan group binding list` (moved from `alert bindings`) and
`vulkan group binding get <topic> <group>`.

**Consequences.** Compile-time breaking renames on the client, JSON
unchanged. `ConsumerConfig.Bindings` keeps its name; the field is the
declaration input, the handle is the stored result. Amends [0644]'s role
list and supersedes [0625]'s `List<Child>` clause outright.

Rejected: a top-level `binding` CLI noun (top-level nouns are client- or
system-level handles); moving the list to `Client` beside `Topics`
(deferred, not refused); `Declare`/`Clear` on the handle.
