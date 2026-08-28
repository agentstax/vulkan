# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Doc-content sweep: page size and interweaving links

Applied 2026-08-28 on approval: transactional-produce split — its
side-effects and retries sections moved to the new
guides/side-effects-and-retries (board row added, parent links out);
link pass over quickstart (step 6 -> the produce guide), migrations
(VK0022/VK0023/VK0053 threads), ordering (architecture, lifecycle,
both compare pages), routing (fan-out), fan-out (consumer-timeouts,
dead-letters), dead-letters <-> replay, rabbitmq-sqs (lifecycle),
why-vulkan capability table (fan-out, routing, ordering). Missing
compaction + workers pages parked in ROADMAP Later. Awaiting review.
