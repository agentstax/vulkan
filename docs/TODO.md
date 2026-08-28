# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Example attribute values on code threads (doc site)

Rung 1 built (from ROADMAP Now, the [0590] self-demonstration gap):
shared example-value table in src/pages/_thread/example-line.ts (one
value per registry attribute name, VK0023 override mirroring VK0022),
composition faithful to Error()'s one-liner and the text-handler line,
round-trip test proving every composed line fills its own paste
placeholders. LogLine's blank-marking path deleted as dead (all
placeholders now fill); markPlaceholders helper deleted with it.
Decision record owed at close-out. Awaiting user review.
