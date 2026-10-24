import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useId, useState } from "react";
import type {
  ComputedAttachment,
  Condition,
  ListenerAttachment,
  ParentStatus,
  SpecParentRef,
} from "../../../api/types";
import { listenerCopy, routeCopy, viewACopy } from "../../../copy/explorer";
import { ComputedBadge, NotVerified } from "../../../explore/ComputedBadge";
import { ConditionList } from "../../../explore/ConditionList";
import { LinkedName } from "../../../explore/LinkedName";
import { objectPath } from "../../../explore/objectPath";
import { InlineLink, TextButton } from "../../../states/controls";
import { asText } from "./refs";

// region Styles
const Card = styled.article<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  align-content: start;
  gap: ${(p) => p.theme.space(2)};
  min-width: 0;
  padding: ${(p) => p.theme.space(3)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  h3 {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px ${(p) => p.theme.space(2)};
    margin: 0;
    font-size: ${(p) => p.theme.font.size.base};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    overflow-wrap: anywhere;
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
    font-weight: ${(p) => p.theme.font.weight.regular};
  }
  .others {
    display: grid;
    gap: ${(p) => p.theme.space(2)};
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .controller {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
    color: ${(p) => p.theme.color.textMuted};
  }
  .hint {
    font-size: ${(p) => p.theme.font.size.sm};
    color: ${(p) => p.theme.color.textMuted};
  }
  ${(p) => p.stylesOverride}
`;
const Computed = styled.div`
  display: grid;
  gap: ${(p) => p.theme.space(1)};
  font-size: ${(p) => p.theme.font.size.sm};
  .label {
    color: ${(p) => p.theme.color.textMuted};
  }
  ul {
    display: grid;
    gap: ${(p) => p.theme.space(2)};
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: grid;
    gap: 2px;
  }
  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px ${(p) => p.theme.space(2)};
    overflow-wrap: anywhere;
  }
`;
const Toggle = styled(TextButton)`
  justify-self: start;
`;

// region Helpers
/** One listener the parentRef attaches to: a Gateway listener opens the Gateway page with it selected (design §8.1). */
function ListenerItem({ installId, l }: { installId: string; l: ListenerAttachment }) {
  const to = l.resource.kind === "Gateway" ? objectPath(installId, l.resource) : undefined;
  // Hostnames are sent only when checked or disagrees; there an absent or empty list means any hostname.
  const known = l.computed.state === "checked" || l.computed.state === "disagrees";
  const hosts = l.effectiveHostnames?.length ? l.effectiveHostnames.join(", ") : listenerCopy.anyHostname;
  return (
    <li data-testid={`computed-listener-${l.listener}`}>
      <span className="line">
        {to ? (
          <InlineLink to={`${to}?listener=${encodeURIComponent(l.listener)}`}>{l.listener}</InlineLink>
        ) : (
          l.listener
        )}
        <ComputedBadge computed={l.computed} />
      </span>
      {known ? (
        <span className="line">
          <span>
            <span className="label">{viewACopy.effectiveHostnames}:</span> {hosts}
          </span>
          {/* Which hostnames, unlike the count, no status reports (rules §8.4). */}
          <NotVerified testId="hostnames-not-verified" />
        </span>
      ) : null}
    </li>
  );
}

/** View A's result for the parentRef: outcome and explanation, then the listeners it attaches to. */
function ComputedResult({ installId, a }: { installId: string; a: ComputedAttachment }) {
  return (
    <Computed>
      <div className="line" data-testid="computed-outcome">
        <span className="label">{viewACopy.computedOutcome}:</span>
        {a.outcome ? <span>{viewACopy.outcomes[a.outcome] ?? a.outcome}</span> : null}
        <ComputedBadge computed={a.computed} />
        {a.explanation ? <span>{a.explanation}</span> : null}
      </div>
      {a.listeners.length ? (
        <>
          <span className="label">{viewACopy.computedListeners}</span>
          <ul>
            {a.listeners.map((l) => (
              <ListenerItem key={`${l.resource.kind}/${l.resource.name}/${l.listener}`} installId={installId} l={l} />
            ))}
          </ul>
        </>
      ) : null}
    </Computed>
  );
}

// region Component
interface ParentCardProps {
  installId: string;
  parentRef: SpecParentRef;
  /** status.parents entries for this parentRef, ours and other controllers'. */
  entries: ParentStatus[];
  generation: number;
  /** The route has a delegation ref, so a BackendNotFound may be about it. */
  hasDelegation: boolean;
  /** The entries' ref is in no spec parentRef: "implicit" when parentRefs is empty, else "unlisted". */
  origin?: "implicit" | "unlisted";
  /** View A's result for this parentRef (2.3); only Gateway and ListenerSet parentRefs have one. */
  attachment?: ComputedAttachment;
  stylesOverride?: SerializedStyles;
}

/** One spec parentRef: its target, section or port, and every condition this install's controllers wrote. */
export function ParentCard({
  installId,
  parentRef,
  entries,
  generation,
  hasDelegation,
  origin,
  attachment,
  stylesOverride,
}: ParentCardProps) {
  const [open, setOpen] = useState(false);
  const listId = useId();
  const ours = entries.filter((e) => e.ours);
  const others = entries.filter((e) => !e.ours);
  // The server links the target on the status entry and the spec ref; an older body without either is text.
  const target = entries[0]?.target ?? parentRef.target ?? asText(parentRef);
  const name = `${parentRef.namespace}/${parentRef.name}`;
  // Interim wording (Task 31): with a delegation ref, BackendNotFound may not mean a missing Service.
  const extra = (c: Condition) =>
    hasDelegation && c.type === "ResolvedRefs" && c.status === "False" && c.reason === "BackendNotFound" ? (
      <span className="hint">{routeCopy.mixedParentHint}</span>
    ) : null;
  const label =
    origin === "implicit"
      ? routeCopy.implicitParentCard(name)
      : origin === "unlisted"
        ? routeCopy.notInParentRefsCard(name)
        : routeCopy.parentCard(name);
  return (
    <Card aria-label={label} stylesOverride={stylesOverride}>
      <h3>
        {origin ? (
          <span className="muted">{origin === "implicit" ? routeCopy.implicitParent : routeCopy.notInParentRefs}</span>
        ) : null}
        <span className="muted">{parentRef.kind}</span>
        <LinkedName refValue={target} installId={installId} />
        {parentRef.sectionName ? (
          <span className="muted">
            {routeCopy.section} {parentRef.sectionName}
          </span>
        ) : null}
        {parentRef.port ? (
          <span className="muted">
            {routeCopy.port} {parentRef.port}
          </span>
        ) : null}
      </h3>
      {ours.length ? (
        ours.map((e) => (
          <ConditionList key={e.controllerName} conditions={e.conditions} generation={generation} extra={extra} />
        ))
      ) : (
        <span className="hint">{routeCopy.noOwnStatus}</span>
      )}
      {attachment ? <ComputedResult installId={installId} a={attachment} /> : null}
      {others.length ? (
        <>
          <Toggle
            type="button"
            aria-expanded={open}
            aria-controls={open ? listId : undefined}
            onClick={() => setOpen((o) => !o)}
          >
            {routeCopy.otherControllers(others.length)}
          </Toggle>
          {open ? (
            <ul className="others" id={listId}>
              {others.map((e) => (
                <li key={e.controllerName}>
                  <span className="controller">{e.controllerName}</span>
                  <ConditionList conditions={e.conditions} generation={generation} />
                </li>
              ))}
            </ul>
          ) : null}
        </>
      ) : null}
    </Card>
  );
}
