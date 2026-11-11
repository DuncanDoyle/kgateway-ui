import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { Check, Copy, Download } from "lucide-react";
import { Fragment, type ReactNode, useEffect, useRef, useState } from "react";
import type { SuggestedFix as SuggestedFixData } from "../api/types";
import { diagnosticsCopy, findingsCopy, fixCopy } from "../copy/explorer";
import { LinkedName } from "../explore/LinkedName";
import { Facts, Section } from "../pages/explore/detailParts";
import { layout } from "../shell/layout";
import { VisuallyHidden } from "../states/VisuallyHidden";

// region Styles
const Body = styled.div<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: ${(p) => p.theme.space(3)};
  max-width: ${layout.proseMax};
  p {
    margin: 0;
  }
  code {
    font-family: ${(p) => p.theme.font.mono};
    font-size: 0.95em;
  }
  .mono {
    font-family: ${(p) => p.theme.font.mono};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
  .code-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: ${(p) => p.theme.space(1)};
    margin-bottom: ${(p) => p.theme.space(1)};
  }
  .code-head h3 {
    flex: 1;
    margin: 0;
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  pre {
    margin: 0;
    padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
    overflow-x: auto;
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.text};
    background: ${(p) => p.theme.color.surfaceRaised};
    border: 1px solid ${(p) => p.theme.color.border};
    border-radius: ${(p) => p.theme.radius.sm};
  }
  ${(p) => p.stylesOverride}
`;
/** A labelled action on a code block; the label keeps its width when the icon swaps, so nothing moves. */
const Action = styled.button`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 10px;
  cursor: pointer;
  font: inherit;
  font-size: ${(p) => p.theme.font.size.sm};
  white-space: nowrap;
  color: ${(p) => p.theme.color.text};
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    border-color: ${(p) => p.theme.color.text};
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 6%, ${(p) => p.theme.color.surface});
  }
  &:active {
    background: color-mix(in srgb, ${(p) => p.theme.color.text} 14%, ${(p) => p.theme.color.surface});
  }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;

// region Helpers
/** How long the copied tick and announcement stay, as in NextSteps. */
const COPIED_MS = 3000;

/** text with each `quoted` part as code; the server marks names that way. */
export function withCode(text: string): ReactNode[] {
  // Keyed by character offset, which is stable for a given text.
  let at = 0;
  return text.split("`").map((part, i) => {
    const key = at;
    at += part.length + 1;
    return i % 2 ? <code key={key}>{part}</code> : <Fragment key={key}>{part}</Fragment>;
  });
}

/** The file a fix downloads as, e.g. referencegrant-allow-shop.yaml; a fragment gets -fix. */
export const fixFileName = (fix: SuggestedFixData) =>
  `${fix.target.kind.toLowerCase()}-${fix.target.name}${fix.create ? "" : "-fix"}.yaml`.replace(/[^a-z0-9._-]/gi, "-");

function download(text: string, file: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "application/yaml" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = file;
  a.click();
  // Revoked after the click has handed the URL to the download.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

// region Component
interface SuggestedFixProps {
  fix: SuggestedFixData;
  installId: string;
  /** The heading's id, unique per page when several fixes show. */
  headingId?: string;
  stylesOverride?: SerializedStyles;
}

/**
 * The one change the evidence leaves (design §7.7, §17.2): the object, the fragment's location, the YAML to copy or
 * download, and the basis. A new object is created with kubectl create, which fails on a taken name (R89).
 */
export function SuggestedFixView({ fix, installId, headingId = "finding-fix", stylesOverride }: SuggestedFixProps) {
  const [copied, setCopied] = useState<string>();
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const file = fixFileName(fix);
  const copy = () => {
    // Clearing first makes a repeated copy announce again.
    setCopied(undefined);
    clearTimeout(timer.current);
    const done = (m: string) => {
      setCopied(m);
      timer.current = setTimeout(() => setCopied(undefined), COPIED_MS);
    };
    const write = navigator.clipboard?.writeText(fix.yaml) ?? Promise.reject(new Error("no clipboard"));
    write.then(
      () => done(diagnosticsCopy.copied),
      () => done(diagnosticsCopy.copyFailed),
    );
  };
  const CopyIcon = copied === diagnosticsCopy.copied ? Check : Copy;
  return (
    <Section aria-labelledby={headingId}>
      <h2 id={headingId}>{findingsCopy.suggestedFix}</h2>
      <Body stylesOverride={stylesOverride}>
        <p>{withCode(fix.title)}</p>
        <Facts>
          <dt>{fixCopy.object}</dt>
          <dd>
            {fix.target.kind} <LinkedName refValue={fix.target} installId={installId} />
            {fix.create ? <div className="muted">{fixCopy.newObject}</div> : null}
          </dd>
          {!fix.create && fix.path ? (
            <>
              <dt>{fixCopy.location}</dt>
              <dd>
                <span className="mono">{fix.path}</span>
                <div className="muted">{fixCopy.merge}</div>
              </dd>
            </>
          ) : null}
          <dt>{fixCopy.basis}</dt>
          <dd>{withCode(fix.basis)}</dd>
        </Facts>
        <div>
          <div className="code-head">
            <h3>{fixCopy.yaml}</h3>
            <Action type="button" aria-label={fixCopy.copyLabel(fix.title)} onClick={copy}>
              <CopyIcon size={14} aria-hidden="true" />
              {fixCopy.copy}
            </Action>
            <Action type="button" aria-label={fixCopy.downloadLabel(file)} onClick={() => download(fix.yaml, file)}>
              <Download size={14} aria-hidden="true" />
              {fixCopy.download}
            </Action>
          </div>
          <pre>{fix.yaml}</pre>
        </div>
        {fix.create ? (
          <div>
            <div className="code-head">
              <h3>{fixCopy.createWith}</h3>
            </div>
            <pre>{`kubectl create -f ${file}`}</pre>
            <div className="muted">{withCode(fixCopy.createNote)}</div>
          </div>
        ) : null}
      </Body>
      <VisuallyHidden role="status" aria-live="polite">
        {copied ?? ""}
      </VisuallyHidden>
    </Section>
  );
}
