# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.
## BindingHandle

Split `SystemHandle.ListBindingDeclarations` into a group-scoped handle.
Every binding row is keyed by consumer_group_id inside one topic's
tables, so the handle's identity is (topic, group). Only `Get` ships
now; `Waiting`, `Log`, `Matches` are ROADMAP Later.

Renamed 2026-09-03: the read-model `BindingDeclaration` is `Binding` all
the way down (consume, controller, admin, vulkan, files `binding.go`),
and the `<Noun>Declaration` suffix row left CONVENTIONS -- it had no
other user. The decision record (step 10) records both. Log-row names
stay: `BindingOutcome`, `BindingConfigLogRow`, `DeclareBindings`,
`NewestInstalledDeclaration`.

Names follow the existing patterns, no new ones. The client layer is
bare nouns: a handle is named for the read-model its `Get` returns
(`GroupHandle` -> `Group`), the parent constructor is that noun singular
(`Group(name)`, `Alert(key)`), and a list is the bare plural
(`Topics`, `Schedulers`, `Groups`, `Alerts`, `Messages`) -- never
`List*` on a handle (settled 2026-09-03; the four existing `List*`
handle verbs are drift, fixed in step 6). `List<Noun>s` / `Get<Noun>`
are admin, controller, and datastore verbs (`ListGroups`, `GetGroup`);
the ConsumeController qualifies its verbs by noun. The CLI nests a
group child under `group` the way `group config get` does.

    client.System().Bindings(ctx)                  // was ListBindingDeclarations; stays on System
    client.Topic("orders").Group("receipts").Binding().Get(ctx)

Build order, each step foreground-checked (build, `go test -race` on
the touched package):

- [x] 1. Datastore rename (established-bad-name rule): `ListBindingLog` /
      `listBindingLog` / `listTopicBindingLog` / `ListGroupBindingLog` /
      `listGroupBindingLog` -> `*BindingConfigLog`, plus `BindingLogStatus`
      / `BindingLogInstalled` / `BindingLogWaiting` ->
      `BindingConfigLogStatus` / `BindingConfigLogInstalled` /
      `BindingConfigLogWaiting`, matching the table and
      `BindingConfigLogRow` since [0611] (precedent:
      `appendTopicConfigLog`, `appendWorkerConfigLog`).
- [x] 2. Datastore `ConsumeDatastore.ListGroupBindingLog(ctx, topicId,
      groupId) ([]BindingConfigLogRow, error)`: public Wrap pair over
      `listTopicBindingLog`. Parent-child list name after admin's
      `ListGroupWorkers`.
- [x] 3. Controller `ConsumeController.GetBinding(ctx, topicId, groupId)
      (*consume.Binding, error)` in controller/binding.go: id guards, one
      datastore read, `NewestInstalledDeclaration`, `toBinding`;
      `(nil, nil)` when no installed row -- the group reads the whole
      topic.
- [x] 4. Admin `MessageAdmin.GetBinding(ctx, topicName, groupName)` in
      admin/binding.go beside `ListBindings`: resolves ids the way
      `GetGroup` does, `(nil, nil)` when the topic or group is absent.
- [x] 5. pkg/vulkan `binding.go`: `BindingHandle{topicName, groupName,
      client}`, `GroupHandle.Binding()` (no I/O, no args -- the group is
      the identity, like `Client.System()`), `Get(ctx)` comma-ok.
      `SystemHandle.ListBindingDeclarations` -> `Bindings` (stays on
      System). alias.go unchanged; closure test green.
- [x] 6. Handle `List*` drift, same rule: `GroupHandle.ListWorkers` ->
      `Workers`, `SchedulerHandle.ListMessages` -> `Messages`,
      `TopicHandle.ListKeyMessages` -> `KeyMessages`; CLI, lab, and
      client.mdx callers updated.
- [x] 7. Rename `BindingDeclaration` -> `Binding` down the stack; files
      `binding.go`; `<Noun>Declaration` suffix row dropped from
      CONVENTIONS.
- [ ] 8. CLI -- `group binding` sub-noun beside `group config`:
      group_binding.go, group_binding_list.go (the fleet list, moved from
      alert_bindings.go, which is deleted and alert.go's Short trimmed),
      group_binding_get.go (`group binding get <topic> <group>`, laid out
      like group_config_get.go).
- [ ] 9. Docs -- routing.mdx `vulkan alert bindings` -> `vulkan group
      binding list`, plus a `Binding().Get` sample; client.mdx already
      carries the new names (step 6), re-read once the CLI lands.
- [ ] 10. Lab: one fresh-DB lab driving `Binding().Get` before and after a
      consumer's Register installs a set, and the nil case (bindinglab
      already exercises `System().Bindings`; extend it).
- [ ] 11. Decision record (next number after current max) covering the
      handle, the bare-plural client rule, the `Binding` rename and the
      dropped suffix row; HISTORY entry; remove this section.
