---
status: accepted
date: 2026-08-20
phase: pre-v1
---

# 0554 -- Plain-error construction standard

## Context

[0550] defined the declared-error anatomy and [0553] the declaration
boundary, but the ~609 in-scope plain errors below that boundary had no
written construction rules. A sweep found the drift: 33 constraint guards
missing the violating value while same-file neighbors carried it, static
messages built with fmt.Errorf, a two-param guard fused into one clause,
"unhandled" wording, and a declared error (VK0004) raised through
fmt.Errorf prose instead of With.

## Decision

CONVENTIONS.md ## Errors gains "When writing a plain error": the
problem-line templates, banned words, and tense rules apply identically --
a plain error is the same fact minus code, recovery, and registry; a
constraint guard ends `, got <value>` (%d ints, %v durations, %q strings)
while absence guards carry no value clause; names are spelled as the
caller knows them (param, exported field, stored column/JSON key);
errors.New for static text; ` -- <fix>` clauses allowed under the
fix-writing rules, a fix naming another package's method or a CLI command
being the promotion tell; wrapping adds only the owned fact, spelled as
declared.

Swept project-wide. Vendored robfig, examples/, reference/, and the three
unexported control-flow variables are exempt. The audit also surfaced the
third instance of [0553]'s deleted-mid-declaration race class, missed by
its audit: cron's replaceConfig, promoted to cron.ErrDeclarationInterrupted
VK0025 (Transient, mirroring worker VK0024 -- DatastoreRetry re-runs the
declare and re-creates the row).

## Consequences

One docs page (VK0025). Plain-error wording is now citable, so review
comments point at a rule instead of taste. One raise site stays
unresolved in TODO.md: the binding-declaration datastore detects group
absence but cannot import controller where ErrGroupNotFound lives -- the
consumer stack declares its errors in the controller, not a vocabulary
package the datastore can reach. (Resolved same-day by [0555]: the
declarations moved to the pkg/consumergroup vocabulary root and the
datastore raises ErrGroupNotFound directly.)
