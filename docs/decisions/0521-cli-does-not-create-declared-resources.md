---
status: accepted
date: 2026-08-15
phase: "14"
---

# 0521 — Only a declarer writes config, so the CLI creates nothing (amends 0518, 0520)

**Context.** [0518] deleted every CLI config-mutation verb but kept `register`
in the surviving list, and `vulkan topic register` still carried the five
config flags. That leaves the hole the sweep was meant to close: `vulkan topic
register orders --retention-ttl 1h` is `topic config set` under another verb
name, and the next deploy silently reverts it. `vulkan cron register` is worse
-- `--schedule` is required, so the command cannot exist without writing
config. The codebase already had the answer: there is no `vulkan system
register`. A system is created only by calling admin.RegisterSystem from Go;
the CLI has `system get` and `system destroy` and nothing else.

**Decision.**
- Only a declarer writes config, and the CLI is not a declarer. A declarer
  re-asserts its config on every boot; a human running a one-off command does
  not, so anything the CLI wrote would be overwritten by the next deploy.
- Creation follows declaration: `vulkan topic register` and `vulkan cron
  register` are deleted. System's shape becomes the shape of every resource.
- Dropping only the flags was rejected for both. A cron job cannot exist
  without a schedule. A flagless topic register would declare library defaults
  and clobber an existing topic's declared config, and making it create-only
  instead would leave two creation semantics -- CLI create-only beside code
  latest-wins -- which is the second-mechanism smell the sweep exists to
  remove.
- Destroy stays while create goes. Destroy is an operational verb acting on
  something that already exists and has no declaration to contend with; the
  asymmetry is the point, not an oversight.

**Consequences.** The CLI surface per resource becomes read plus act: system
`get`/`destroy`; topic `list`/`get`/`config get`/`rename`/`destroy`; cron
`list`/`get`/`suspend`/`unsuspend`/`run`/`destroy`. cmd/vulkan/internal/cli/
register.go is deleted here; cron_register.go joins cron_alter.go in [0520]'s
chunk, taking `fieldDiff` with it. cmd/vulkan/README.md's register section is
replaced by a note on where topics come from. A topic or cron job can no
longer be brought into existence without running Go -- intended for a team
that owns both the app and the database, where everything lives in git, and
the cost is that there is no shell path to poke a topic into being.
