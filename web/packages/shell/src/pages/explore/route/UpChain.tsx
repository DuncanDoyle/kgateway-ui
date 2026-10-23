import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { EyeOff, Repeat } from "lucide-react";
import type { AncestorPath, LinkedRef } from "../../../api/types";
import { LinkedName } from "../../../explore/LinkedName";
import { ResourceName } from "../../../resource/ResourceName";
import { sameRef } from "./refs";

// region Styles
const Nav = styled.nav<{ stylesOverride?: SerializedStyles }>`
  margin-bottom: ${(p) => p.theme.space(4)};
  font-size: ${(p) => p.theme.font.size.sm};
  ol {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .paths {
    display: grid;
    gap: 4px;
  }
  .steps {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px 6px;
    min-width: 0;
  }
  .steps > li {
    display: inline-flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .sep,
  .kind,
  .note {
    color: ${(p) => p.theme.color.textMuted};
  }
  [aria-current="page"] {
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .hidden,
  .cycle {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: ${(p) => p.theme.color.textMuted};
  }
  .note {
    margin: 4px 0 0;
  }
  ${(p) => p.stylesOverride}
`;

// region Component
interface UpChainProps {
  installId: string;
  /** The route this page shows: its own step is the current page, not a link. */
  self: LinkedRef;
  paths: AncestorPath[];
  truncated: boolean;
  stylesOverride?: SerializedStyles;
}

/** Paths from a Gateway or ListenerSet down to this route, root first, over reported edges (design §5.3). */
export function UpChain({ installId, self, paths, truncated, stylesOverride }: UpChainProps) {
  return (
    <Nav aria-label="Route parents" stylesOverride={stylesOverride}>
      {paths.length ? (
        <ol className="paths">
          {paths.map((p, i) => (
            // Paths have no identity of their own; the server's order is stable.
            // biome-ignore lint/suspicious/noArrayIndexKey: positional
            <li key={i} data-testid="route-path">
              <ol className="steps">
                {p.steps.map((s, j) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: a path may repeat a route (cycle)
                  <li key={j}>
                    {j > 0 ? (
                      <span className="sep" aria-hidden="true">
                        ›
                      </span>
                    ) : null}
                    {!s.ref ? (
                      // Restricted: no identity at all (design §7.4 rule 3).
                      <span className="hidden">
                        <EyeOff size={14} aria-hidden="true" />A parent you cannot view
                      </span>
                    ) : j === p.steps.length - 1 && sameRef(s.ref, self) ? (
                      <span aria-current="page">
                        <ResourceName namespace={s.ref.namespace} name={s.ref.name} />
                      </span>
                    ) : (
                      <>
                        <LinkedName refValue={s.ref} installId={installId} />
                        <span className="kind">({s.ref.kind})</span>
                      </>
                    )}
                  </li>
                ))}
                {p.cycle ? (
                  <li className="cycle">
                    <Repeat size={14} aria-hidden="true" />
                    Cycle
                  </li>
                ) : null}
              </ol>
            </li>
          ))}
        </ol>
      ) : (
        <p className="note">No reported path to a Gateway.</p>
      )}
      {truncated ? <p className="note">More paths not shown</p> : null}
    </Nav>
  );
}
