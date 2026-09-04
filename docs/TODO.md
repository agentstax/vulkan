# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.
## BindingDeclarationHandle

Split `SystemHandle.ListBindingDeclarations` into a group-scoped handle.
Every binding row is keyed by consumer_group_id inside one topic's
tables, so the handle's identity is (topic, group). Only `Get` ships
now; `Waiting`, `Log`, `Matches` are ROADMAP Later.

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

    client.System().BindingDeclarations(ctx)                  // was ListBindingDeclarations; stays on System
    client.Topic("orders").Group("receipts").BindingDeclaration().Get(ctx)

Build order, each step foreground-checked (build, `go test -race` on
the touched package):

1. Datastore rename first (established-bad-name rule): `ListBindingLog`
   / `listBindingLog` / `listTopicBindingLog` -> `ListBindingConfigLog`
   / `listBindingConfigLog` / `listTopicBindingConfigLog`, matching the
   table and `BindingConfigLogRow` since the [0611] rename.
2. DONE 2026-09-03 (as `ListGroupBindingLog`; step 1 renames it) -- `ConsumeDatastore.ListGroupBindingConfigLog(ctx,
   topicId, groupId) ([]BindingConfigLogRow, error)`: public Wrap pair
   over `listTopicBindingConfigLog` (its `groupId` filter already
   exists, used only inside the declare transaction today). Parent-child
   list name after admin's `ListGroupWorkers`.
3. DONE 2026-09-03 -- `ConsumeController.GetBindingDeclaration(ctx, topicId,
   groupId) (*consume.BindingDeclaration, error)` in
   controller/binding_declaration.go: id guards, one datastore read,
   `NewestInstalledDeclaration`, `toBindingDeclaration`; `(nil, nil)`
   when no installed row -- the group reads the whole topic.
4. DONE 2026-09-03 -- `MessageAdmin.GetBindingDeclaration(ctx, topicName,
   groupName)` in admin/binding.go beside `ListBindingDeclarations`:
   resolve ids the way `GetGroup` does, `(nil, nil)` when the topic or
   group is absent.
5. DONE 2026-09-03 (alias.go unchanged, closure test green) -- new `binding_declaration.go` (file named for the
   handle's noun, like group.go / alert.go):
   `BindingDeclarationHandle{topicName, groupName, client}`,
   `GroupHandle.BindingDeclaration()` (no I/O, no args -- the group is
   the identity, like `Client.System()`), `Get(ctx)` comma-ok.
   `SystemHandle.ListBindingDeclarations` -> `BindingDeclarations`
   (stays on System). `tools/conventions` closure test decides whether
   alias.go changes.
6. DONE 2026-09-03 (CLI, labs, client.mdx callers updated) -- handle `List*` drift, same rule: `GroupHandle.ListWorkers` ->
   `Workers`, `SchedulerHandle.ListMessages` -> `Messages`,
   `TopicHandle.ListKeyMessages` -> `KeyMessages`; update their CLI
   and lab callers and client.mdx mentions.
7. CLI -- `group binding` sub-noun beside `group config`:
   group_binding.go, group_binding_list.go (the fleet list, moved from
   alert_bindings.go, which is deleted and alert.go's Short trimmed),
   group_binding_get.go (`group binding get <topic> <group>`, laid out
   like group_config_get.go).
8. Docs -- client.mdx: the handles list and the "where the old verbs
   went" table (`ListBindingDeclarations` mentions plus the step-6
   renames); routing.mdx `vulkan alert bindings` -> `vulkan group
   binding list`, plus the `Get` sample.
9. Lab: one fresh-DB lab driving `BindingDeclaration().Get` before and
   after a consumer's Register installs a set, and the nil case.
10. Decision record (next number after current max), HISTORY entry,
    remove this section.
