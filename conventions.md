# Conventions

Codebase-wide rules. Violations are bugs, not style nits.

each func param has explicit type, never combined

## SQL

- Never `SELECT *` -- always name columns explicitly. A column ADD must be
  invisible to live binaries built before the column existed; `SELECT *` makes
  even additive schema changes breaking (pgx errors when the field count no
  longer matches the scan destination count). Explicit column lists are what
  keep adds non-breaking, leaving column removal as the only change that needs
  the two-release expand/contract dance.
