# Notes for the user documentation

Topics the user-facing documentation must cover when it is written. Each entry says what to explain and why. Add an entry whenever a decision creates something an operator needs to know.

## Delegation edge cap and memory (2026-10-27, D-025)

- **What it is.** The UI computes route delegation (which child routes each parent selects) up to a cap on selection edges per install. An edge is one parent ref selecting one child route. A wildcard or label ref that matches many routes makes many edges.
- **What users see past the cap.** Parents past the cap read "too large to compute". Rows already computed keep their outcomes with "Some paths not computed". Checks that report something missing (a missing child status entry, the computed KGWUI-011) are not raised anywhere in the install. View A and the controller's own status are unaffected.
- **Caps are shared.** The caps count the whole install, routes the viewer cannot see included. So a large hidden part can make a visible route read "too large to compute".
- **Depth.** A delegation path deeper than 100 routes is also "too large to compute". This bound is fixed.
- **Inherited headers.** A child that inherits its parent's matcher copies every header and query match above it. Very deep inherit chains with many headers therefore reach the matcher cap sooner and read "too large to compute" earlier.
- **How to raise it.** The cap is configurable (binary flag and chart value; see the chart README). Raising it lets the UI compute denser delegation, but memory grows with it: during a recompute the old and new models are both in memory. Users who raise the cap must also raise the pod's memory limit. The binary sets its Go memory target to 90 % of the pod limit unless `GOMEMLIMIT` is set, so the two move together.
- **Sizing guidance.** Give measured numbers (peak RSS per cap value on the dense benchmark, `docs/phases/phase-2/bench/`) rather than a formula. Measured on the densest benchmark (100 wildcard parents × 1,000 routes, no `GOMEMLIMIT`; `bench/2.4-dense-memory.txt`): the default 10,000 peaks at about 425–465 MiB RSS, inside the default 512Mi limit; 20,000 at about 510–565 MiB; 50,000 at about 700–760 MiB. Less dense installs use less at the same cap.
