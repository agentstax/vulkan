---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0472 — "Firing" is retired from the codebase's vocabulary

**Context.** "Firing" is Quartz-scheduler jargon, and the house rule is to name things as what they literally are in this codebase rather than borrow another domain's terms.

**Decision.** The cron domain uses `JobRequest`, `ScheduledTime`, "due", and "produce" — never "fire". The alert domain followed in the same sweep: alert `Status` value `'firing'` became `'active'` (`'resolved'` stays), and the repeat-interval republish is called a "repeat", never a "re-fire".

**Consequences.** One vocabulary across code, CLI, and records; readers meet the codebase's own nouns instead of translating scheduler jargon. The sweep covered every occurrence, per the rule that an established bad name gets fixed everywhere, not just in new code.
