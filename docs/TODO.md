# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Ordered delivery per key [0617] (ROADMAP Now, Step 1 gap 3)

Proposal page: website/src/content/docs/guides/ordered-delivery.mdx
(PROPOSED). Chunks, each green on its own:

1. Rename `allow`/`defer` -> `parallel`/`exclusive` everywhere
   (identifiers, stored option string, docs, sandbox, Vale, Vocabulary
   rows). No behavior change.
2. exception_queue `message_key` + `concurrency` + index, written by
   every inserter; exception claim's lease exclusion drops its
   message_log join; sandbox mirror; ExceptionData scan. No behavior
   change.
3. `ordered`: enum value + produce guards (key required, refuses
   Compaction.Enable); two-predicate ordered key-lease claim (own range
   excluded from the window); exception claim predicate for ordered
   rows; both runner gates; lab: fail-then-succeed keeps order, dead
   releases the lane, two instances.
4. In-range lane: `buffered.next` chain built in add, run back-to-back
   under one permit, failure defers the rest of the chain; lab: order
   + throughput inside one range at MessageConcurrency 4.
5. Ship: page un-proposed, ordering.mdx aside + comparison table,
   HISTORY, ROADMAP sub-bullet removed; playground 10 is Step 2.
