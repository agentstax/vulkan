---
status: accepted
date: 2026-08-19
phase: pre-v1
---

# 0550 -- Structured error anatomy: five parts + recovery, flat codes

## Context

Error text was per-raise-site fmt.Errorf prose: no stable identifier, no
machine-readable values, retryability implied by wording, the CLI unable to
render fields, JSON log queries reduced to regex. Research across Postgres
(ERROR/DETAIL/HINT + SQLSTATE), rustc (code + labels + help + --explain),
Stripe/Google (code/message/param/doc_url), and agent-repair studies
(enumerating legal values raised LLM self-correction 36-44pp) converged on
one anatomy.

## Decision

One struct in pkg/common carries: code (VK + four digits, flat serial,
append-only -- same scheme as decision records), problem (fact), values
(named pairs), fix (advice), docs (URL derived from code), recovery
(Transient | Permanent enum, declared with the error). Domain errors.go
files declare each condition as a named Err* variable via common.NewError;
raise sites attach values only. errors.Is identity = code. One renderer per
surface: Error() one-liner (code as link, no URL), slog.LogValuer for JSON
logs, a single CLI errorHandler branch for the block and --output json.
Tense follows recovery, test-enforced. The fix is rewritten per surface (Go
API in the library, verbatim-pasteable vulkan command in the CLI);
code/problem/values never change across surfaces.

The CONVENTIONS.md ## Errors section is written to the 2026 agent-authoring
evidence: canonical example first (models copy examples literally),
task-scoped headers, one atomic testable rule per line with a one-clause
reason, concrete never-list, renderer mechanics delegated to code rather
than restated as prose (rule-count budget dominates adherence).

Rejected: class-grouped codes (SQLSTATE style) -- pre-v1 domain churn
strands classes and codes never renumber; interpolated-prose values --
forecloses CLI field rendering and JSON queryability; docs URL in the
one-liner -- doubles wrapped-chain length for a derivable fact; the word
"sentinel" -- retired everywhere for "named error variable".

## Consequences

Retry machinery gains a first check on recovery and stops on Permanent;
RetryableError/PermanentError marker types fold into the one mechanism.
Existing fmt.Errorf raise sites and errors.New declarations migrate in a
sweep. CONVENTIONS.md gained ## Errors and the Package layout wording
change. The docs site gains one page per code, headed by the verbatim
problem text. Implementation (common.Error, renderers, tense test, sweep)
is follow-on work planned via TODO.md when picked up.
