# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## consumerFunc hard-timeout guide (doc site)

New small page guides/consumer-timeouts (from ROADMAP Now, doc-site
content owed), placement decided this session: standalone guide with
interweaving links per user preference. Covers the cancel -> grace ->
abandon window with real values, the ctx-respecting handler fix, the
two knobs (Timeout vs TimeoutGrace), and the observability trail.
Inbound links added from VK0041/VK0050/VK0052 and concepts/lifecycle;
Guides board row added. Drafted, awaiting user review.
