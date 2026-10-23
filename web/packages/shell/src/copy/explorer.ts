/**
 * User-facing copy of the explorer pages (Phase 2.2), from the wording pass (Task 31).
 * The ids in comments (S01…) match the wording record in docs/phases/phase-2/phase-2.2-wording.md.
 */

type Noun = readonly [string, string];
const plural = (n: number, [one, many]: Noun) => `${n} ${n === 1 ? one : many}`;

// region Shell and navigation
export const navCopy = {
  overview: "Overview", // S01
  gateways: "Gateways",
  routes: "Routes",
  listenerSets: "ListenerSets",
  findings: "Findings", // S02
  label: "Install", // S03
  copyCommand: "Copy kubectl command", // S04
};

// region Lists and tables
export const listCopy = {
  gateways: { title: "Gateways", search: "Filter Gateways" }, // S07
  routes: { title: "Routes", search: "Filter routes" },
  listenerSets: { title: "ListenerSets", search: "Filter ListenerSets" },
  searchByName: "Filter by name or namespace", // S08
  searchRoutes: "Filter by name, namespace or hostname", // S09
  health: "Health", // S10
  routeState: "Route state", // S11
  removeFilter: (tag: string) => `Remove filter ${tag}`, // S20
  clearFilters: "Clear filters", // S21
  empty: (many: string) => `No ${many} in this install.`, // S22
  emptyFiltered: (many: string) => `No ${many} match these filters.`, // S23
  denied: (n: number, noun: Noun) => `You cannot view ${plural(n, noun)} in this install.`, // S24
  pages: "Pages",
  previous: "Previous", // S25
  next: "Next",
  previousPage: "Previous page",
  nextPage: "Next page",
  cannotLoad: (what: string) => `Cannot load ${what}`, // S26
  toggleDetails: (open: boolean, name: string) => `${open ? "Hide" : "Show"} details for ${name}`, // S27
  details: "Details", // S28
  notInScope: "Not in this install's scope",
  backToInstall: "Back to install",
};

/** Column headers and cell placeholders. */
export const columnCopy = {
  status: "Status",
  gateway: "Gateway",
  class: "Class",
  listeners: "Listeners",
  attachedRoutes: "Attached routes", // S29
  notAccepted: "Routes not accepted", // S13
  listenerSets: "ListenerSets",
  addresses: "Addresses",
  route: "Route",
  kind: "Kind",
  parents: "Parents",
  hostnames: "Hostnames",
  childRoutes: "Child routes",
  listenerSet: "ListenerSet",
  parentGateway: "Parent Gateway",
  why: "Why", // S54
  none: "None", // S30
  inherited: "Inherited", // S31
  mirror: "Mirror", // S35 (fixed)
};

/** Route state filter chips (design §6.1). */
export const routeStateCopy = {
  accepted: "Accepted", // S12
  "not-accepted": "Not accepted", // S13
  "refs-unresolved": "References not resolved", // S14
  "not-programmed": "Rules replaced or dropped", // S15 (fixed)
  pending: "Waiting for status", // S16
  declared: "No status entry for the parent route", // S17
  "no-parent": "No parent", // S18
};

/** How a parent took a route. */
export const attachmentCopy = {
  accepted: "Accepted", // S12
  "not-accepted": "Not accepted", // S13
  pending: "Waiting for status", // S16
  unwatched: "Namespace not watched", // S32
  declared: "No status entry for the parent route", // S17
};

/** Tags for filters set through the URL (S19). */
export const tagCopy = {
  namespace: "Namespace",
  kind: "Kind",
  parent: "Parent",
  attachment: "Attachment",
  backend: "Backend",
};

export const parentLineCopy = {
  onGateway: "on Gateway", // S33
  onHiddenGateway: "on a Gateway you cannot view", // S34
};

// region Detail pages (shared)
export const detailCopy = {
  overview: "Overview", // S05
  yaml: "YAML",
  views: (kind: string) => `${kind} views`, // S06
  summary: "Summary",
  conditions: "Conditions",
  noConditions: "No conditions reported.", // S39
  stale: (observed: number, generation: number) => `Describes generation ${observed}; the object is at ${generation}.`, // S37
  bannerLead: "Not part of this install. Shown because", // S94
  bannerFinding: "a finding of this install names it.", // S95
  bannerRefersTo: "refers to it.",
  bannerObject: "an object of this install refers to it.",
  backendLookup: "Routes that send traffic here", // S96 (fixed)
};

// region Gateway page
export const gatewayCopy = {
  listeners: "Listeners",
  listenerSets: "ListenerSets",
  attachedListenerSets: (n: number) => `Attached ListenerSets (status): ${n}`, // S49
  noListenerSets: "No ListenerSets name this Gateway.", // S50
  attachedRoutes: "Attached routes", // S51
  noRoutesAttached: "No routes attached.",
  notAttached: "Routes not attached", // S52
  notAttachedGroup: "Not attached",
  noRoutesInState: "No routes in this state.", // S55
  subFilter: {
    "not-accepted": "Not accepted", // S53
    pending: "Waiting for status",
    unwatched: "Namespace not watched",
  },
};

export const listenerCopy = {
  table: "Listeners",
  listener: "Listener",
  port: "Port",
  protocol: "Protocol",
  hostname: "Hostname",
  tls: "TLS",
  allowedRoutes: "Allowed routes",
  noStatusWritten: "The controller has not written status for this listener.", // S40
  noStatus: "No status", // S41
  notSet: "Not set", // S42
  anyHostname: "Any hostname", // S43
  notReported: "Not reported", // S44
  sameNamespace: "Same namespace", // S45
  allNamespaces: "All namespaces",
  namespacesWith: (selector: string) => `Namespaces with ${selector}`,
  anyLabels: "Any labels", // S46
  anyLabelsInline: "any labels",
  defaultKinds: "Default kinds for the protocol", // S47
  toggle: (open: boolean, name: string) => `${open ? "Hide" : "Show"} conditions for listener ${name}`, // S48
};

// region ListenerSet page
export const listenerSetCopy = {
  parent: "Parent",
  parentGateway: "Parent Gateway",
  notAvailable: "Not available", // S56
  allowedTitle: "Parent Gateway's", // S57, followed by the field name
  allowedField: "allowedListeners",
  allowedUnset: "Not set: the Gateway allows no ListenerSets.", // S58
  // S59: the field name sits between the two parts.
  crdHintBefore: "If you set",
  crdHintAfter:
    "and it is missing here, the Gateway CRD in this cluster may not have the field. The API server drops fields its CRD does not define. If so, install a Gateway CRD from the same Gateway API channel and version as the ListenerSet CRD.",
};

// region HTTPRoute page
export const routeCopy = {
  upChain: "Route parents", // S61
  hiddenParent: "A parent you cannot view", // S62
  cycle: "Cycle", // S63
  truncated: "More paths not shown", // S64
  noPath: "No reported path to a Gateway or ListenerSet.", // S65
  hostnames: "Hostnames",
  inheritsListener: "Inherits the listener's hostnames", // S66
  inheritsParent: "Inherits the parent route's hostnames",
  parentRoutes: "Parent routes", // S67
  allowsAny: "Allows any parent route", // S68
  allows: (parents: string[]) => `Allows: ${parents.join(", ")}`, // S69
  allowsNone: "Allows no parent route", // S70
  parents: "Parents",
  parentCard: (name: string) => `Parent ${name}`,
  section: "Section",
  port: "Port",
  noOwnStatus: "No status from this install's controllers.", // S71
  otherControllers: (n: number) => `Other controllers (${n})`, // S72
  mixedParentHint: "A backend may be missing, or a delegation ref may select no route.", // S73
  noParentRefs: "No parentRefs.", // S74
  rules: "Rules",
  noRules: "No rules.", // S75
  rule: (n: number) => `Rule ${n}`,
  matches: "Matches",
  filters: "Filters",
  backendRefs: "Backend refs", // S76
  noBackendRefs: "No backend refs.", // S77
  backendRefFilters: "Filters on this backend ref", // S78
  defaultMatch: "Default match", // S79
  header: "Header",
  query: "Query",
  method: "Method",
  mirrorTo: "Mirror to", // S80
  weight: (n: number, defaulted?: boolean) => `Weight ${n}${defaulted ? " (default)" : ""}`, // S81
  restricted: "Restricted", // S82
  otherRoutes: "Other routes using this backend", // S83 (fixed)
  backendAccepted: "Configuration accepted", // S84 (fixed)
  backendStale: "Describes an older generation of the Backend.", // S85
  backendNoEvidence: "The controller does not say which version it checked.", // S86, after "Accepted: True."
  delegatesTo: "Delegates to", // S87
  delegateRoute: "Route", // S88
  delegateWildcard: (ns: string) => `Every route in ${ns}`,
  delegateLabel: (ns: string, label: string) => `Routes in ${ns} with label ${label}`,
  delegation: {
    reported: "Reported by the controller", // S89
    "not-reported": "Not reported by the controller", // S90
    "not-found": "No such route", // S91
    restricted: "Restricted", // S82
  },
  childRoutes: "Child routes",
  toggleChildren: (open: boolean) => (open ? "Hide child routes" : "Show child routes"), // S92
  noChildRoutes: "No reported child routes.", // S93
};
