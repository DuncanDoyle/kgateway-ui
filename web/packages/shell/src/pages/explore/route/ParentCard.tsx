import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { useId, useState } from "react";
import type { Condition, ParentRef, ParentStatus } from "../../../api/types";
import { routeCopy } from "../../../copy/explorer";
import { ConditionList } from "../../../explore/ConditionList";
import { LinkedName } from "../../../explore/LinkedName";
import { TextButton } from "../../../states/controls";
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
const Toggle = styled(TextButton)`
  justify-self: start;
`;

// region Component
interface ParentCardProps {
  installId: string;
  parentRef: ParentRef;
  /** status.parents entries for this parentRef, ours and other controllers'. */
  entries: ParentStatus[];
  generation: number;
  /** The route has a delegation ref, so a BackendNotFound may be about it. */
  hasDelegation: boolean;
  /** The entries' ref is in no spec parentRef: "implicit" when parentRefs is empty, else "unlisted". */
  origin?: "implicit" | "unlisted";
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
  stylesOverride,
}: ParentCardProps) {
  const [open, setOpen] = useState(false);
  const listId = useId();
  const ours = entries.filter((e) => e.ours);
  const others = entries.filter((e) => !e.ours);
  // The target's link comes from the status entry; without one, the spec ref is text.
  const target = entries[0]?.target ?? asText(parentRef);
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
