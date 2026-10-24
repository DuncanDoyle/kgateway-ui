import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Info } from "lucide-react";
import type { Candidate } from "../api/types";
import { detailCopy } from "../copy/explorer";
import { LinkedName } from "./LinkedName";

// region Styles
const Banner = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: flex;
  align-items: flex-start;
  gap: ${(p) => p.theme.space(2)};
  margin-bottom: ${(p) => p.theme.space(3)};
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  font-size: ${(p) => p.theme.font.size.sm};
  line-height: 1.5;
  color: ${(p) => p.theme.color.text};
  background: color-mix(in srgb, ${(p) => p.theme.color.accentText} 8%, ${(p) => p.theme.color.surface});
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.md};
  overflow-wrap: anywhere;
  svg {
    flex: none;
    margin-top: 3px;
    color: ${(p) => p.theme.color.accentText};
  }
  ${(p) => p.stylesOverride}
`;

// region Component
interface CandidateBannerProps {
  candidate: Candidate;
  installId: string;
  stylesOverride?: SerializedStyles;
}

/** Says why an object the install does not own has a page (design §7.1); it sits above the page header. */
export function CandidateBanner({ candidate, installId, stylesOverride }: CandidateBannerProps) {
  const why =
    candidate.reason === "finding" ? (
      detailCopy.bannerFinding
    ) : candidate.by ? (
      <>
        {candidate.by.kind} <LinkedName refValue={candidate.by} installId={installId} /> {detailCopy.bannerRefersTo}
      </>
    ) : (
      detailCopy.bannerObject
    );
  return (
    <Banner role="note" data-testid="candidate-banner" stylesOverride={stylesOverride}>
      <Info size={16} aria-hidden="true" />
      <span>
        {detailCopy.bannerLead} {why}
      </span>
    </Banner>
  );
}
