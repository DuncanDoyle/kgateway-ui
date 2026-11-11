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
  problemsOnly: "Problems only", // S771
  find: "Find a node", // S772
  findPlaceholder: "Find a listener, route or backend", // S773
  found: (at: number, of: number) => `${at} of ${of}`, // S774
  notFound: "No match", // S775
  large: (routes: number, backends: number) =>
    `This Gateway has ${routes} routes and ${backends} backends, so the map shows problems only. Narrow by kind or namespace, or clear Problems only to load every path.`, // S790
  inconsistent: "The paths changed while loading. The map may be out of date.", // S777
  panel: "Node details", // S778
  close: "Close details", // S779
  fail: (n: number, of: number) => `${n} of ${of} paths through this node fail`, // S780
  warn: (n: number, of: number) => `${n} of ${of} paths through this node have a warning`, // S781
  unknown: (n: number, of: number) => `${n} of ${of} paths through this node are unknown`, // S782
  healthy: (of: number) => `All ${of} paths are healthy`, // S783
  healthyOne: "Its one path is healthy", // S792
  problems: "Problems", // S784
  noProblems: "Nothing on this node itself.", // S785
  endpoints: "Endpoints", // S786
  port: (port: number) => `Port ${port}:`, // S787
  open: "Open details", // S788
  traffic: "Show these paths in Traffic", // S789
  partial: (shown: number, total: number) =>
    `Showing ${shown.toLocaleString("en-US")} of ${total.toLocaleString("en-US")} paths. Loading the rest.`, // S791
  shown: (shown: number, total: number) =>
    `Showing ${shown.toLocaleString("en-US")} of ${total.toLocaleString("en-US")} paths.`, // S793
  nodeLabel: (kind: string, name: string, state: string) => `${kind} ${name}, ${state}`, // S794
  keys: "Arrow keys move along the paths. Enter opens details. Escape closes them.", // S795
  keyboard: "Keyboard", // S796
  table: "Table", // S797
  tableTip: "Show these paths as a table", // S798
  tableLabel: "Paths through this Gateway", // S799
};
