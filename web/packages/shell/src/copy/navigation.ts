/**
 * User-facing copy of the breadcrumb trail, the object header and the data-age line (Phase 3.1), from the wording pass (Task 31).
 * The ids in comments match the wording record in docs/phases/phase-3/phase-3.1-wording.md.
 */

// region Breadcrumbs
export const breadcrumbsCopy = {
  label: "Breadcrumb", // S295: the trail nav
  more: (n: number) => `Show ${n} earlier pages`, // S296: the "…" button
  menu: "Earlier pages", // S297
  // S298: the server's "none" link covers hidden and absent objects, so the label claims neither.
  hidden: "Page not available",
};

// region Object header
export const objectHeaderCopy = {
  namespace: "namespace:", // S299
};

// region Data age
export const dataAgeCopy = {
  fresh: (ago: string) => `Last checked ${ago}`, // S300
  stale: (ago: string) => `Data is stale. Last checked ${ago}`, // S301
};
