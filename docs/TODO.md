# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Error / package-kinds remainder

- [ ] Open: standing repo-walk banned-word test over plain raise strings
      (0553 called the audit repeatable) -- decide with user.

## Error anatomy close-out remainder

- [ ] After the next `just site-deploy`: confirm the deployed /errors/ pages
      resolve at the Docs() URLs (exact-case /errors/VK0005), then drop the
      placeholder TODO comment on docsBaseURL in pkg/common/error.go.
