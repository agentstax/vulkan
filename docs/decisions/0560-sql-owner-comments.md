# 0560 -- SQL literal owner comments

status: accepted
date: 2026-08-20
phase: pre-v1

## Context

Postgres's own observability surfaces (pg_stat_statements, the server
log's slow-statement lines) show query text with no link back to the
library verb that ran it. Rails' QueryLogs/marginalia proved the fix:
put the application identity inside the SQL as a comment.

## Decision

Every SQL literal's first line is a comment naming its owner:
`-- vulkan: <package>.<method>` (CONVENTIONS.md ## SQL). <package> is the
domain/worker segment (topic, janitor, messageconsumer), never the
literal Go package name "datastore"; <method> is the private method that
runs the query, or the shared const's base name.

## Consequences

pg_stat_statements rows and server-log statements attribute load to
library verbs with zero runtime cost -- the text is constant per query,
so pgx statement caching is unaffected (comments ship once per
connection at prepare time, per the fanOut verification of 2026-08-13).
Unlike marginalia the values are static: no per-request identifiers, so
cardinality and cache safety cannot regress. Swept across all datastore
packages and migration DDL in the same change.
