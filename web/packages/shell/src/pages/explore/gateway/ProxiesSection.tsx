import type { LinkedRef, ProcessSet } from "../../../api/types";
import { diagnosticsCopy, processesCopy } from "../../../copy/explorer";
import { ProcessTable } from "../../../diagnostics/ProcessTable";
import { Section } from "../detailParts";
import { refText } from "../listParts";

// region Component
interface ProxiesSectionProps {
  gateway: LinkedRef;
  proxies: ProcessSet;
}

/** A Gateway's proxy processes. Pods the caller may not view are left out by the server; the counts stay (§14 rule 16). */
export function ProxiesSection({ gateway, proxies }: ProxiesSectionProps) {
  const counts =
    proxies.observed && !proxies.restricted && proxies.ready !== undefined && proxies.desired !== undefined
      ? diagnosticsCopy.readyOf(proxies.ready, proxies.desired)
      : undefined;
  return (
    <Section aria-labelledby="gw-proxies">
      <h2>
        <span id="gw-proxies">{processesCopy.proxies}</span>
        {counts ? <span className="note">{counts}</span> : null}
      </h2>
      <ProcessTable set={proxies} label={processesCopy.proxiesOf(refText(gateway))} />
    </Section>
  );
}
