---
status: accepted
date: 2026-08-20
phase: pre-v1
---

# 0556 -- Standing walks over plain raise strings

## Context

[0554]'s plain-error rules were convention-only: declared errors have
test-enforced tense/banned-word walks in internal/errorregistry, but a
plain error that drifts off-template (or restates a declared problem, as
the VK0014 duplicate did) surfaced only in manual audits. [0553] called
the audit repeatable; the wording half was not yet standing.

## Decision

internal/errorregistry gains plain_errors_test.go: an ast-based walk
(go/parser, string-literal first args of errors.New / fmt.Errorf) over
pkg/, otelvulkan/, and cmd/vulkan/, skipping _test.go and vendored
robfig. Four mechanical rules, each reporting every violation with
file:line: banned words + "!" (the regexp shared with the declared
walk); static messages built with fmt.Errorf; identifier-led constraint
guards (`^<name> must be `) lacking `, got ` -- absence-shaped nil/zero
constraints excluded, they have no violating value; and any literal
containing a registered problem verbatim (raise the declared variable
instead).

Judgment rules stay review-time: tense (plain errors declare no
recovery), name spelling, fix quality. No marker escape hatch in
production code -- a legitimate exception gets an explicit file:line
entry in the test with its reason.

## Consequences

The 0553/0554 audits' wording half is now a ratchet: all four rules
pass on the current tree, so any future violation is a test failure
naming the site. New raise-string patterns that deserve exemption cost
a visible test edit, reviewed like any other change.
