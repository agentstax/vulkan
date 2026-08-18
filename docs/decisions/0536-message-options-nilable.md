---
status: accepted
date: 2026-08-18
phase: 14b
---

# MessageOptions is the sanctioned nilable sparse sub-document

## Context

[0533] made field absence the zero value and banned pointer-encoded
optionality, with a consequence line that read-model Options fields must be
never-nil. The follow-through was built: NULLIF($n, '{}'::jsonb) on both
message insert branches, COALESCE(m.options, '{}') at all four claim
selects, config-side zero-struct defaults, and Fill/Clamp/Equal stripped of
nil-handling. It worked (labs green) -- and was rolled back.

## Decision

- *common.MessageOptions stays nilable end to end (user-settled): nil means
  "no options document -- the consumer decides everything". Spreading
  NULLIF/COALESCE across every options SQL site is a worse trade than the
  handful of nil folds it deletes.
- Fill, Clamp, WithDefaults, and Validate keep their nil tolerance: they ARE
  the resolution boundary where sparse user input (ProduceOptions.Message,
  config Message/MessageMin/MessageMax) is normalized. Retry inside stays a
  sparse *RetryPolicy for the same reason.
- This narrows [0533]'s consequence line: the zero-value-absence rule and
  the pointer-optional ban stand for scalars, but a sparse sub-document
  whose absence is a meaningful whole (options, a retry policy) is the
  sanctioned pointer-shaped absence, mirrored by a nullable column.

## Consequences

- The options column stays nullable JSONB with NULL as the only empty
  representation; reads hand nil through unchanged.
- Nil guards on Options (CallSafely's requested check, adapter folds) are
  correct and stay -- do not re-propose deleting them or the never-nil
  reshape.
