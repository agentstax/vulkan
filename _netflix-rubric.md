# Netflix engineering-quality rubric

A grading instrument for holding code to the standard "would a Netflix senior engineer
(L5) consider the author a peer." Built 2026-08-12 from five research passes: the June
2024 culture memo, the 2022 leveling system, 2023–2026 interview accounts (candidate and
interviewer side), Netflix Tech Blog engineering-practice posts, direct code analysis of
ten Netflix OSS repos (Hystrix, Zuul, Conductor, Eureka, Metaflow, Dispatch, bpftop,
Hollow, Atlas, SafeTest), and profiles of twelve named Netflix engineers.

---

## 1. Calibration — what the bar actually is

- **The unit of measure is the L5 Senior Software Engineer**: median total comp
  ~$490–590K, nearly all cash, set individually at "personal top of market." For ~25
  years this was the *only* IC title; hiring is still overwhelmingly L5+. There is **no
  downleveling** — a candidate below the L5 bar is rejected, not offered L4. The keeper
  test ("knowing everything I know today, would I hire this person again?") makes the bar
  a continuous condition of employment.
- **The peer profile** (from 12 named engineers — Gregg, Christensen, Husain, Tuulos,
  Goyal, Jones, Rosenthal, Bakker, Gallatin, Fernandez, Kolodny, Hsiao et al.): at
  minimum one artifact strangers depend on — a library with real external adoption,
  upstream commits to shared infrastructure, a book, a standards contribution — produced
  by **owning one hard production problem end-to-end and generalizing the solution**.
  Roughly 60% were already field-known at hire; the other 40% were made by being handed
  problems at a scale almost nobody else has.
- **Quality at Netflix is operational, not textual.** There is no company-wide style
  guide, no mandated code review, no coverage threshold. The bar is enforced by (a)
  senior-only hiring, (b) paved-road tooling so good that off-roading is self-punishing,
  and (c) "operate what you build" — the author is personally paged for their own
  defects. Grade code as if its author will be woken up by it.
- **Judgment is evidenced, in writing.** Every load-bearing claim in Netflix's published
  engineering work is quantified (20% CPU from JDK 17, zero GC pauses from generational
  ZGC, 100K stream-starts/sec load tests, 38M events/sec metrics). Failure gets honest
  public write-ups (the Java 21 virtual-thread deadlock post; a live-streaming series
  written in the shadow of the Paul–Tyson buffering incident).

## 2. How to grade

Score each dimension 1–5, then render a **binary verdict Netflix-style**: consensus
pass/fail, no score averaging, no downlevel. A confirmed 1–2 on any of the three core
dimensions (D1 design, D2 operability, D3 correctness) is a strong "no" that sinks the
whole evaluation regardless of the rest — exactly as one weak onsite round does.

Level anchors used throughout:

| Score | Meaning |
|---|---|
| 1 | Below professional bar — would not pass any round |
| 2 | Competent (L4-ish) — works, but Netflix rejects rather than downlevels |
| 3 | **The bar (L5)** — production habits are reflexive; a Netflix senior would sign off |
| 4 | Above bar (L6/staff) — designed seams, shipped operability tooling, others build on it |
| 5 | Field-shaping (L7 / the named-engineer tier) — the abstraction could be extracted and adopted by strangers |

Weights (mirroring the loop, where system design is the heaviest round, coding the
lightest, and culture — here, judgment legible in the code — is 40–50%):
**D1 30% · D2 20% · D3 15% · D4 10% · D5 10% · D6 10% · D7 5%** — then the binary
override above.

---

## 3. Dimensions

### D1 — Design & abstraction quality (30%)

The heaviest-weighted round. Netflix's design interview is a conversational design
review: requirements first, tradeoffs narrated, pushback survived.

Checkable markers (from the OSS ground truth):
- **One deliberate core abstraction** carries the system: Hystrix's command +
  circuit-breaker, Zuul's small-interface filter chain with annotation-driven ordering,
  Metaflow's decorator-compiled DAG, Atlas's stack-language vocabulary objects. The
  grader should be able to name the abstraction in one sentence; if the code has no
  nameable center, that's a 2.
- **Extension points are designed, not accreted**: interface + package-private impl +
  factory/builder (HystrixCircuitBreaker/Impl/Factory); SPI seams for every integration
  point (Hollow's Publisher/Announcer/Validator); fluent builders on the public API. Flag
  soup and boolean parameters accreting onto one entry point score low.
- **Complexity pools deliberately**: Netflix tolerates one orchestration god-file at the
  engine's heart (AbstractCommand 2,219 lines, WorkflowExecutor 1,817) *while everything
  around it stays small*. Penalize diffuse complexity everywhere, not one acknowledged
  engine core.
- **Requirements clarified before structure chosen**; alternatives named and rejected
  with reasons ("why this over that" survives pushback). ~25% of design-round failures
  are jumping to components before asking about load, latency, and freshness — the code
  analog is machinery sized for a load profile nobody stated.
- Scale/availability/security thought through at the level the problem warrants —
  "design Netflix for Netflix": the depth must come from the problem actually owned, not
  framework recitation.

### D2 — Operability & production-readiness (20%)

"Full cycle developers": done means operable — the author is the one paged.

- **Metrics are part of the API surface, not an afterthought**: Spectator imports in
  51/255 Zuul core files; Conductor's Monitors facade documenting *when to use which
  metric type*; Hollow's per-lifecycle-phase metrics packages; Hystrix shipping five
  metrics-publisher modules. Instrumentation pervasive at the bar; at 4–5 the code ships
  its own debugging tools (Hollow explorer/diff UI, Hystrix dashboard, one-click flame
  graphs).
- **Errors designed for the consumer**: typed error hierarchies with operational meaning
  (NotPrimaryMidCycleException; Metaflow's 16 exception subclasses each with a
  user-facing headline). bpftop's REVIEW.md states the rule form: no unwrap/expect on
  fallible runtime paths, context on every error, resources wrapped so they cannot leak
  on error paths.
- **Contracts stated where the consumer reads**: idempotency spelled out per method
  ("It is idempotent and does not modify any internal state" vs "This is
  non-idempotent"), thread-safety, ordering semantics.
- Timeouts, backpressure, graceful degradation, capacity thinking present where the
  domain demands them (shed optional features, protect the core path — the live-streaming
  playbook).
- **Verification-in-production compatibility**: the design admits canary comparison,
  failure injection, and rollback (statistical canary judgment replaced eyeballing at
  Netflix; code that can only be validated by hope scores low).

### D3 — Correctness under concurrency & failure (15%)

- **Races are closed and narrated**: HystrixCircuitBreaker walks the reader through its
  putIfAbsent race; Eureka's javadoc explains why expiries must not replicate as
  cancellations. Comment density in their Java correlates with shared-state danger
  (22–37% in the concurrent cores), not with file count.
- **Failure modes enumerated, not discovered**: what happens on partial failure,
  duplicate delivery, restart mid-operation, dependency timeout. Chaos-engineering
  culture assumes components fail; code that assumes the happy path is below bar.
- **Explicit edge-case handling over clever one-liners** — the literal interviewer-side
  evaluation axis. Interviewers hand candidates broken code and poke at logic mid-debug;
  the code equivalent is: can a hostile reader find the unhandled edge in five minutes?
- **It must actually work.** Polish does not rescue an unsolved problem — candidates who
  don't finish are rejected regardless of hygiene. An elegant skeleton with a stubbed
  core is a failing grade, not a promising one.

### D4 — Testing proportional to risk (10%)

Netflix has no flat coverage rule; test investment tracks correctness risk. Measured
ratios: resilience library ~1:1 (Hystrix 0.99 test:main), infrastructure 0.6–0.8 (Zuul
0.64, Conductor 0.79, Atlas 0.77), platform 0.3–0.6, end-user app 0.15 plus E2E.

- At the bar: the dangerous code is the tested code — judgment visible in *where* tests
  concentrate, unit tests written alongside the code (they're expected even live in
  interviews), integration coverage for the seams.
- Above bar: **matrix/generative harnesses for combinatorial cores** (Metaflow
  synthesizes test flows from graph-shape specs × execution contexts), benchmarks in CI
  where performance is a feature (Zuul runs JMH per-PR), and shipping test infrastructure
  to your consumers (hystrix-junit).
- Below bar: coverage theater — mock-heavy tests that assert the mock; tests that
  restate the implementation; an untested concurrent core beside well-tested trivia.

### D5 — Readability & naming (10%)

- **Long, explicit, unabbreviated names**: PeerAwareInstanceRegistry,
  ExecutionDAOFacade, HystrixObservableCollapser. Package/module-by-domain.
  Convention-uniform structure — Dispatch's ~50 domain slices each with identical
  models/service/views/flows files, so seeing one teaches all.
- **Comments state the why the code can't show** — concurrency gotchas, distributed
  invariants, stated contracts — and nothing else. Their sparse-comment codebases
  (Dispatch at 3%) substitute structure for prose; both modes pass, what fails is
  narration of the obvious or cleverness needing translation.
- "Readability, sensible abstractions, and explicit edge case handling rather than
  clever one-liners" — verbatim the interviewer axis. Single-author cleverness
  (SafeTest's thenable-proxy trick) is tolerated in a personal tool, penalized in shared
  code.
- Provenance honesty: vendored/borrowed code credited in place ("# stolen from
  getsentry/sentry"), licenses and headers maintained.

### D6 — Evidenced judgment & measurement (10%)

The code-legible form of the culture bar (judgment, candor, curiosity):

- **Measurement is the argument**: performance claims carry before/after numbers;
  capacity choices trace to load figures; no unmeasured "faster"/"scalable" in names,
  comments, or docs.
- **Tradeoffs written down where decisions live**; honest costs stated (the full-cycle
  post lists its own downsides; Hystrix's maintenance notice names the technical reason
  static thresholds lost to adaptive limits). Sunshining in code form: the known wart is
  documented, not hidden.
- Boring-where-possible: vendoring over new dependencies, paved-road defaults over
  novelty, new machinery only where the problem demands it. Framework recitation and
  résumé-driven architecture are the "brilliant jerk" of code — capability display at the
  reader's expense, a reliable no-hire.

### D7 — Repo & lifecycle stewardship (5%)

- Operational docs exist where an operator would look (wiki, mkdocs, or book-README —
  the location varies at Netflix, the existence doesn't). CI runs build + tests +
  release; contribution surface stated.
- **Lifecycle honesty**: machine-readable status (OSSMETADATA), sunset notices that name
  the technical rationale and a successor (Hystrix → resilience4j; bpftop's TRANSFER.md).
  Knowing when to stop is graded as engineering quality.
- 2024–2026 marker: review invariants written down as checkable rules (bpftop's
  REVIEW.md: "every unsafe block has a SAFETY comment...") — the current-era form of a
  team's quality contract.

---

## 4. Instant strong-"no" signals (any one can sink the verdict)

1. **Unfinished core, polished periphery** — the unsolved-problem rejection.
2. **Untested or unnarrated shared-state concurrency** in code that has it.
3. **Errors swallowed or stringly-typed** on paths a consumer must handle.
4. **No observability in long-running/production-shaped code** — unoperable by design.
5. **Cleverness requiring translation** where plain structure was available.
6. **Machinery without a stated problem** — scale features for load nobody measured,
   abstraction seams nothing uses.
7. **Unmeasured performance or scalability claims.**
8. **A second parallel mechanism for a solved problem** in the same codebase — the
   design-review pushback failure ("why not the existing path?") in code form.

## 5. Known Netflix warts — do not over-penalize

- One orchestration god-file at the engine heart, small files around it (their
  consistent pattern for 15 years).
- README quality varies wildly (1.2K to 39K); grade whether operational docs exist
  anywhere, not their location.
- Config sprawl at platform edges (Metaflow's 29K env-var file).
- Bus-factor-1 craftsmanship: 1–3 people deep is their norm, with org-standard
  scaffolding around the individual.

## 6. Verdict template

```
D1 Design ............ x/5  (weight 30)
D2 Operability ....... x/5  (weight 20)
D3 Correctness ....... x/5  (weight 15)
D4 Testing ........... x/5  (weight 10)
D5 Readability ....... x/5  (weight 10)
D6 Judgment .......... x/5  (weight 10)
D7 Stewardship ....... x/5  (weight 5)
Strong-no signals: [list or none]
Verdict: HIRE / NO HIRE at L5 (binary — no downlevel)
Keeper question: knowing everything the code shows, would a Netflix
senior fight to keep this person on the team?
```
