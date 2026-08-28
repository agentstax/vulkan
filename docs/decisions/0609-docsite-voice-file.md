---
status: accepted
date: 2026-08-28
phase: pre-v1
---

# 0609 — website/VOICE.md: the doc site's prose voice file

## Context

AI-drafted site prose reads as house style, not as the author. A
research round (2026-08-28) mined ~3,600 hand-typed session messages
plus the raw strata of docs/archive/explain-it-back.md for a voice
profile, and swept the style-imitation literature for what is
actually validated. The findings that shaped the shape: verbatim
samples beat prose style descriptions in every study comparing them,
and gains flatten past ~10 samples (EMNLP 2025 Findings,
arXiv:2509.14543); contrastive same-passage pairs plus an explicit
name-the-differences step beat samples alone (AAAI 2024,
arXiv:2401.17390); ban-lists hold only ~80% at generation time and
decay over long output, so they work as a revision pass, not a
prompt; fine-tuning is the only method shown to fool expert judges
and does not fit this workflow. The realistic ceiling everywhere:
surface register, cheap to edit — not indistinguishable imitation.

## Decision

- website/VOICE.md is a rule file loaded via website/CLAUDE.md,
  binding site prose only — not error/log grammar, decision records,
  or code comments; the root Vocabulary registry outranks it.
- Its contents follow the evidence order: ~15 verbatim samples
  (typos kept, with the standing instruction to correct spelling and
  reproduce everything else), two constructed contrastive pairs with
  the differences named, a measurable-rules section, and a revision
  checklist applied as its own pass after drafting.
- The voice target is the samples' structure and stance — answer
  first, arrow-chain mechanisms, verdicts with costs, admitted
  uncertainty, deadpan tail-clause humor, no closers — with spelling
  and punctuation corrected. Raw typos are fingerprints of the
  source, not the style.

## Consequences

- Constructed pairs are the weakest part; the file says to replace
  them with real before/after edits from shipped pages as those
  accumulate.
- The later workflow rungs stay on the roadmap: author-seeded drafts
  (the author writes the rough take, the AI continues and tightens)
  and periodic mining of draft-to-published git diffs back into this
  file. Both deferred by the author until time allows.
- Success measure is the per-page edit count on AI-drafted prose,
  not any absolute style score — no single judge is reliable for
  style fidelity (arXiv:2508.06374).
