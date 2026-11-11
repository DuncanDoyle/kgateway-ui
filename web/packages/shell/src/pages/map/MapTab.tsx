import { css, type SerializedStyles } from "@emotion/react";
import { lazy, Suspense, useEffect, useMemo } from "react";
import { useLocation } from "react-router-dom";
import type { LinkedRef } from "../../api/types";
import { listCopy } from "../../copy/explorer";
import { mapCopy } from "../../copy/map";
import { summaryCopy, trafficCopy } from "../../copy/traffic";
import { PageState } from "../../states/PageState";
import { Shimmer } from "../../states/Shimmer";
import { StaleGate } from "../traffic/StaleGate";
import { MapBox } from "./map.css";
import { useMapPaths } from "./mapData";

// region Styles
const fill = css`
  height: 100%;
  border-radius: 0;
`;

// region Helpers
// React Flow loads with the map only, as its own chunk (design §6.2); MapTab starts it on mount, beside the first page.
const load = () => import("./GatewayMap");
const GatewayMap = lazy(load);

/** Holds the map's place while its chunk or its first page loads. */
function Fallback() {
  return (
    <MapBox aria-busy="true" aria-label={mapCopy.loading} data-testid="map-fallback">
      <Shimmer stylesOverride={fill} />
    </MapBox>
  );
}

// region Component
interface MapTabProps {
  installId: string;
  gateway: LinkedRef;
  stylesOverride?: SerializedStyles;
}

/** Gateway detail's Map tab: the paths through the Gateway under the URL's filters, drawn as a map (design §6). */
export function MapTab({ installId, gateway, stylesOverride }: MapTabProps) {
  const { search } = useLocation();
  const params = useMemo(() => new URLSearchParams(search), [search]);
  const paths = useMapPaths(installId, `${gateway.namespace ?? ""}/${gateway.name}`, params);
  useEffect(() => {
    void load();
  }, []);
  if (paths.error && !paths.summary)
    return (
      <PageState
        kind="error"
        title={listCopy.cannotLoad(trafficCopy.noun[1])}
        message={paths.error.message}
        copyText={paths.error.message}
      />
    );
  if (paths.summary?.notComputed)
    return <PageState kind="empty" title={summaryCopy.notComputedTitle} message={summaryCopy.notComputed} />;
  if (!paths.summary) return <Fallback />;
  return (
    <StaleGate collectedAt={paths.collectedAt} failed={!!paths.error}>
      <Suspense fallback={<Fallback />}>
        <GatewayMap gateway={gateway} rows={paths.rows} stylesOverride={stylesOverride} />
      </Suspense>
    </StaleGate>
  );
}
