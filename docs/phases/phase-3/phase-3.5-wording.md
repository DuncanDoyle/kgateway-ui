# Sub-phase 3.5 — Wording

3 strings, five options each; 1 new SPA label, 1 new check note, 1 changed selection note.

Cross-model: NO for the picker. The 3.5 session could not start a separate agent, so the author chose, on the same rules as 3.4: plain words, short, accurate, least machine-sounding. The user can change any choice.

Option 1 is the drafted text. Placeholders stay in order; finding codes and kinds are unchanged.

| # | id | File:line | Where | Placeholders | Option 1 (draft) | Option 2 | Option 3 | Option 4 | Option 5 | Choice |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | S597 | `copy/explorer.ts:514` | Overview Data plane cell for a 024 error | none | Config withheld | Updates withheld | No config updates | Updates held back | Withheld | 1 (matches the 024 slug and message) |
| 2 | S598 | `pkg/health/checks.go:101` | Served kinds check note on a list-only change | `%s` the change | Listed kinds changed (%s); restart the UI to list them | Kinds the UI only lists changed (%s); restart the UI to list them | List-only kinds changed: %s. Restart the UI to list them | %s; restart the UI to list these kinds | Kinds shown but not checked changed (%s); restart the UI | 2 ("list-only" is internal jargon) |
| 3 | S182 | `pkg/computed/rulesets/rulesets.go:26` | Selection note, unknown controller version (D-058 item 11) | none | Controller version unknown; using the 2.5 rules | Unknown controller version; using the 2.5 rules | Controller version not known; using 2.5 rules | Can't read the controller version; using the 2.5 rules | Controller version unknown, so the 2.5 rules apply | 1 (keeps the 2.x wording, only the version changes) |
