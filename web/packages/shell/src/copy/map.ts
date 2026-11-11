/**
 * User-facing copy of the Gateway map (Phase 6, sub-phase 6.5).
 * The ids in comments (S766…) match the wording record in docs/phases/phase-6/phase-6.5-wording.md.
 */
export const mapCopy = {
  tab: "Map", // S766
  canvas: "Map of this Gateway's paths", // S767
  loading: "Loading the map", // S768
  truncated: (shown: number, total: number) =>
    `Showing ${shown.toLocaleString("en-US")} of ${total.toLocaleString("en-US")} paths. Narrow the filters to see the rest.`, // S769
  action: "Action", // S770
};
