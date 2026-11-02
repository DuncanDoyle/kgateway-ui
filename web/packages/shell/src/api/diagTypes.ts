import type { LinkedRef, Page } from "./exploreTypes";
import type { Severity } from "./types";

/** Mirrors the diagnostics DTOs of pkg/api/diag_types.go as built (sub-phase 3.1, design §13.3). */

/**
 * One finding of GET …/findings. restricted: the caller may not view the resource; only identity is sent (no message).
 * evidenceRestricted: the detail was replaced because it quotes an object the caller may not view.
 */
export interface FindingItem {
  id: string;
  code: string;
  slug: string;
  severity: Severity;
  resource?: LinkedRef;
  message?: string;
  detail?: string;
  since?: string;
  group?: string;
  restricted?: boolean;
  evidenceRestricted?: boolean;
}
/** One command a user can run to learn more; needs names the permissions it requires, e.g. "pods/log". */
export interface NextStep {
  title: string;
  command: string;
  needs?: string[];
  note?: string;
}
/** The one change the evidence leaves (design §7.7); path is the fragment's location when create is false. */
export interface SuggestedFix {
  title: string;
  target: LinkedRef;
  create: boolean;
  path?: string;
  yaml: string;
  basis: string;
}
/** GET …/findings/{findingId}: affected pages through the visible objects of the finding's group, 50 at a time. */
export interface FindingDetail {
  install: string;
  finding: FindingItem;
  suggestedFix?: SuggestedFix;
  nextSteps?: NextStep[];
  affected: Page<LinkedRef>;
  collectedAt: string;
}
export type FindingList = Page<FindingItem>;
