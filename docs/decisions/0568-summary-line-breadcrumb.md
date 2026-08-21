# 0568 -- Summary lines declare and carry a help breadcrumb

status: accepted
date: 2026-08-21
phase: pre-v1

## Context

The [0567] stopped line ships ten counters whose names are the
codebase's own row statuses -- correct, but not self-describing, and as
an undeclared Info line it carried no pointer to any explanation: every
surface built for legibility (docs page, vulkan explain, Prometheus
HELP) required already knowing it exists. [0562] drew the declaration
boundary at operator-actionable Warn/Error, so lifecycle lines had no
code. Precedents: journald catalogs join explanations to lines by a
stable id at display time; every declared vulkan event already treats
its code as "the greppable pointer"; error fixes name the verbatim
command to run.

## Decision

- The declaration boundary widens ONE notch: a lifecycle summary line
  whose attr set needs a docs page declares despite being Info. The
  stopped line is the first: VK0041, "consumer stopped", empty
  consequence, unexported in pkg/consumer/logs.go (VK0038 precedent --
  API packages hold no vocabulary).
- logStopped logs the code as the first attr pair and a trailing `help`
  attr -- plain words ending in the pasteable command: "metrics
  explained: vulkan explain VK0041". New `help` attr-registry row,
  summary lines only.
- The hand-written VK0041 page is the counter catalog: per counter,
  what it counts, what a nonzero value means, and the flow-vs-level
  warning against reconciling session counters with the fleet gauges.
  It replaces the planned separate session-summary docs page.
- Parked: auto-appending `help` to EVERY coded line via the logging
  pipeline's enrich stage -- one mechanism instead of per-site attrs,
  but it reshapes 12+ existing lines; evaluate on its own merit.

## Consequences

The line is its own breadcrumb: an operator or agent pasting it has the
code to grep, the command to run, and a search hit on the verbatim
message. Extends [0562] (boundary) without superseding it; [0567]'s
line shape gains two attrs. Rejected: prose or URLs in the message
(static-message rule; URL churn pre-rename), relying on journalctl
-x-style tooling alone (needs the annotate mode built first).
