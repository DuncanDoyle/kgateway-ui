import styled from "@emotion/styled";
import { useId, useMemo, useState } from "react";
import { useRoutes } from "../../../api/exploreHooks";
import type { LinkedRef, RouteRow } from "../../../api/types";
import { columnCopy, routeCopy } from "../../../copy/explorer";
import type { ObjectColumn } from "../../../explore/ObjectTable";
import { HealthBadge } from "../../../health/HealthBadge";
import { TextButton } from "../../../states/controls";
import { expectedRows, refParam, Section, SubList } from "../detailParts";
import { Muted, NameCell, OneLine } from "../listParts";

// region Styles
const Toggle = styled(TextButton)`
  margin-bottom: ${(p) => p.theme.space(2)};
`;

// region Helpers
const NOUN = ["route", "routes"] as const;
const rowRef = (r: RouteRow) => r.ref;

function columns(installId: string): ObjectColumn<RouteRow>[] {
  return [
    { id: "health", header: columnCopy.status, width: "120px", cell: (r) => <HealthBadge state={r.health} /> },
    { id: "name", header: columnCopy.route, cell: (r) => <NameCell r={r.ref} installId={installId} /> },
    {
      id: "hostnames",
      header: columnCopy.hostnames,
      width: "24%",
      secondary: true,
      cell: (r) =>
        r.hostnames.length ? <OneLine text={r.hostnames.join(", ")} /> : <Muted>{columnCopy.inherited}</Muted>,
    },
  ];
}

// region Component
interface ChildRoutesProps {
  installId: string;
  route: LinkedRef;
  /** Visible reported children: the rows parent=<route> lists (declared candidates are never children). */
  expected: number;
}

/** Routes under this one, fetched only when opened so a wide delegation does not load the subtree (design §5.3). */
export function ChildRoutes({ installId, route, expected }: ChildRoutesProps) {
  const [open, setOpen] = useState(false);
  const bodyId = useId();
  const query = useMemo(() => ({ parent: refParam(route) }), [route]);
  const cols = useMemo(() => columns(installId), [installId]);
  return (
    <Section aria-labelledby="child-routes">
      <h2 id="child-routes">
        {routeCopy.childRoutes} <span className="note">{expected}</span>
      </h2>
      {expected === 0 ? (
        <Muted>{routeCopy.noChildRoutes}</Muted>
      ) : (
        <>
          <Toggle
            type="button"
            aria-expanded={open}
            aria-controls={open ? bodyId : undefined}
            onClick={() => setOpen((o) => !o)}
          >
            {routeCopy.toggleChildren(open)}
          </Toggle>
          {open ? (
            <div id={bodyId}>
              <SubList
                installId={installId}
                label={routeCopy.childRoutes}
                noun={NOUN}
                useList={useRoutes}
                query={query}
                columns={cols}
                rowRef={rowRef}
                emptyTitle={routeCopy.noChildRoutes}
                skeletonRows={expectedRows(expected)}
              />
            </div>
          ) : null}
        </>
      )}
    </Section>
  );
}
