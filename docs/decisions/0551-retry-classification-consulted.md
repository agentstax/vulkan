---
status: accepted
date: 2026-08-19
phase: pre-v1
---

# 0551 -- Retry classification is consulted, never encoded; one retry type

## Context

Retryability rode marker wrappers: RetryableError/PermanentError wrapped the
real error, classify() re-wrapped every unclassified datastore error, and
call sites needing a per-call override (partition create-ahead lock_timeout,
ambiguous Commit) wrapped by hand. Recovery declared on common.Error ([0550])
made the markers a second classification mechanism. A first cut kept both
types with a classifier func field on Retry, defaulted then overwritten by
NewRetryDatastore -- rejected: bundling-then-overwriting is a smell, and
plain Retry turned out to have zero users besides RetryDatastore.

## Decision

Classification is consulted at the retry loop, never encoded onto the error.
RetryDatastore is the one retry type (plain Retry merged away); its Wrap
calls the exported IsTransientDatastoreError -- recovery declared on the
error decides first, a bare error is judged by IsTransientPgError, anything
else is permanent. Errors surface from Wrap as raised, never rewrapped.
Per-call overrides became declared errors raised with .Wrap(cause):
errPartitionLockTimeout (VK0018, Transient, producer datastore-local
errors.go -- the door-first producer stack makes pkg/producer unreachable
from its datastore) and common.ErrCommitConfirmationLost (VK0019, Permanent,
beside its sibling ErrLeaseLost).

Rejected: classifier func field with default + constructor override
(bundled-then-overwritten); classifier as a NewRetry param plus
NewDefaultRetry (public ceremony for zero existing callers). A
general-purpose retry, if wanted, is designed at the v1 API review.

## Consequences

retry_error.go (RetryableError, PermanentError, IsRetryable) and retry.go
deleted; MIN_DELAY moved beside CalculateDelay. The batcher's
classifyBatchFailure uses IsTransientDatastoreError. multitargetlab asserts
InTransaction surfaces Commit errors with no common.Error wrapping.
