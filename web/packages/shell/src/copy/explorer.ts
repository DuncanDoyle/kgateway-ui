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
  // Status entries no spec parentRef names: an implicit parent (empty parentRefs) or a leftover entry.
  implicitParent: "Implicit parent", // S108
  implicitParentCard: (name: string) => `Implicit parent ${name}`,
  notInParentRefs: "Not in parentRefs", // S109
  notInParentRefsCard: (name: string) => `Parent ${name}, not in parentRefs`,
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
  emptyMatches: "Empty match list", // S110
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

// region Computed facts (2.3). Drafts S111–S133, for the wording pass (Task 48).
export const computedCopy = {
  chip: "Computed", // S111
  tooltip: (ruleSet: string, family: string, rule: string) => `Computed by the UI from ${ruleSet}'s ${family}: ${rule}`, // S112
  attachmentRules: "attachment rules", // S113
  otherRules: "rules", // S114
  ruleNames: {
    "allowed-routes": "allowed routes", // S115
    "section-and-port": "section name and port", // S116
    "hostname-intersection": "hostname intersection", // S117
    "listener-count": "listener route count", // S118
  } as Record<string, string>,
  coverage: {
    "count-agrees": "Count agrees with the controller", // S119
    "status-compatible": "Status agrees", // S120
    "not-verified": "Not checked by the controller", // S121
  },
  pending: "Waiting for the controller", // S122
  restricted: "Hidden: depends on objects you cannot view", // S123
  disagrees: "The UI's computation disagrees with the controller. The controller's status is what counts.", // S124
  unknownReasons: {
    "scope-unknown": "Unknown: the install's scope is not known", // S125
    "namespace-labels-unavailable": "Unknown: namespace labels cannot be read", // S126
    "listenerset-no-status": "Unknown: the ListenerSet has no status yet", // S127
    "setting-unknown": "Unknown: a controller setting cannot be read", // S128
    "not-reached": "Unknown: no root route reaches this route", // S129
    "context-unknown": "Unknown: a route above this one is unknown", // S130
    "too-large": "Unknown: too large to compute", // S131
  } as Record<string, string>,
  unknownOther: (reason: string) => `Unknown: ${reason}`, // S132
  stale: "Unknown: the data is stale", // S133
};

// region View A on pages (2.3). Drafts S134–S163, for the wording pass (Task 48).
export const viewACopy = {
  computedRoutes: "Computed routes", // S134
  isolationNote: "Envoy may serve fewer hostnames on this listener", // S135
  listenerGroup: "Routes by listener", // S136
  allListeners: "All listeners", // S137
  notPlaced: "Not placed", // S138
  routesOnListener: (name: string) => `Routes on listener ${name}`, // S139
  notPlacedTitle: "Routes the UI could not place", // S140
  noRoutesOnListener: "No routes on this listener.", // S141
  noRoutesNotPlaced: "The UI placed every route.", // S142
  cannotList: "The UI cannot list this listener's routes.", // S143
  allRoutesOf: (kind: string) => `All routes of this ${kind}`, // S144
  effectiveHostnames: "Effective hostnames", // S145
  whyNotPlaced: "Why not placed", // S146
  computedListeners: "Listeners", // S147
  computedOutcome: "Computed outcome", // S148
  outcomes: {
    accepted: "Accepted", // S149
    NotAllowedByListeners: "Not allowed by the listeners", // S150
    NoMatchingParent: "No listener matches the section or port", // S151
    NoMatchingListenerHostname: "No listener hostname matches", // S152
  } as Record<string, string>,
  routesColumn: "Routes", // S153
  routesTooltip: "Computed by the UI: distinct routes on this object's own listeners", // S154
  noCount: "No count", // S155
  disagreements: (n: number) => `${n} disagrees`, // S156
  stateFlags: {
    disagrees: "UI disagrees with the controller", // S157
    "listener-unknown": "Listener unknown", // S158
  },
};
