import styled from "@emotion/styled";
import { useId, useMemo, useState } from "react";
import { useRoutes } from "../../../api/exploreHooks";
import type { LinkedRef, RouteRow } from "../../../api/types";
import type { ObjectColumn } from "../../../explore/ObjectTable";
import { HealthBadge } from "../../../health/HealthBadge";
import { TextButton } from "../../../states/controls";
import { expectedRows, refParam, Section, SubList } from "../detailParts";
import { attachmentLabel, Muted, NameCell } from "../listParts";
import { sameRef } from "./refs";

// region Styles
const Toggle = styled(TextButton)`
  margin-bottom: ${(p) => p.theme.space(2)};
`;

// region Helpers
const NOUN = ["route", "routes"] as const;
const rowRef = (r: RouteRow) => r.ref;

function columns(installId: string, parent: LinkedRef): ObjectColumn<RouteRow>[] {
  return [
    { id: "health", header: "Status", width: "120px", cell: (r) => <HealthBadge state={r.health} /> },
    { id: "name", header: "Route", cell: (r) => <NameCell r={r.ref} installId={installId} /> },
    {
      id: "attachment",
      header: "Attachment",
      width: "200px",
      cell: (r) => {
        const p = r.parents.find((x) => sameRef(x.ref, parent));
        return p ? attachmentLabel[p.attachment] : <Muted>Reported by the controller</Muted>;
      },
    },
    {
      id: "hostnames",
      header: "Hostnames",
      width: "24%",
      secondary: true,
      cell: (r) => (r.hostnames.length ? r.hostnames.join(", ") : <Muted>Inherited</Muted>),
    },
  ];
}

// region Component
interface ChildRoutesProps {
  installId: string;
  route: LinkedRef;
  /** Visible reported children plus declared candidates: the rows parent=<route> lists. */
  expected: number;
}

/** Routes under this one, fetched only when opened so a wide delegation does not load the subtree (design §5.3). */
export function ChildRoutes({ installId, route, expected }: ChildRoutesProps) {
  const [open, setOpen] = useState(false);
  const bodyId = useId();
  const query = useMemo(() => ({ parent: refParam(route) }), [route]);
  const cols = useMemo(() => columns(installId, route), [installId, route]);
  return (
    <Section aria-labelledby="child-routes">
      <h2 id="child-routes">
        Child routes <span className="note">{expected}</span>
      </h2>
      {expected === 0 ? (
        <Muted>No child routes.</Muted>
      ) : (
        <>
          <Toggle
            type="button"
            aria-expanded={open}
            aria-controls={open ? bodyId : undefined}
            onClick={() => setOpen((o) => !o)}
          >
            {open ? "Hide child routes" : "Show child routes"}
          </Toggle>
          {open ? (
            <div id={bodyId}>
              <SubList
                installId={installId}
                label="Child routes"
                noun={NOUN}
                useList={useRoutes}
                query={query}
                columns={cols}
                rowRef={rowRef}
                emptyTitle="No child routes."
                skeletonRows={expectedRows(expected)}
              />
            </div>
          ) : null}
        </>
      )}
    </Section>
  );
}
