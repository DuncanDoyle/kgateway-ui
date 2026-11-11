import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";
import { ArrowRight } from "lucide-react";
import type { PortChain as Chain } from "../../api/types";
import { portChainCopy } from "../../copy/backends";
import { Section } from "../explore/detailParts";

// region Styles
const Chains = styled.ol<{ stylesOverride?: SerializedStyles }>`
  display: grid;
  gap: ${(p) => p.theme.space(2)};
  margin: 0;
  padding: 0;
  list-style: none;
  ${(p) => p.stylesOverride}
`;
const Steps = styled.li`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(2)};
  padding: ${(p) => p.theme.space(2)} ${(p) => p.theme.space(3)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  font-size: ${(p) => p.theme.font.size.sm};
  .step {
    display: inline-flex;
    flex-direction: column;
  }
  .label {
    font-size: ${(p) => p.theme.font.size.xs};
    color: ${(p) => p.theme.color.textMuted};
  }
  .value {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
  }
  .arrow {
    color: ${(p) => p.theme.color.textMuted};
  }
  .fact {
    flex-basis: 100%;
    color: ${(p) => p.theme.color.textMuted};
  }
`;

// region Helpers
/** The target as written: a name (never resolved to a container port) or a number. */
function targetText(c: Chain): string | undefined {
  if (c.targetName) return portChainCopy.named(c.targetName);
  return c.targetPort;
}

// region Component
function Step({ label, value }: { label: string; value: string }) {
  return (
    <span className="step">
      <span className="label">{label}</span>
      <span className="value">{value}</span>
    </span>
  );
}

const Arrow = () => <ArrowRight className="arrow" size={14} aria-hidden="true" />;

/**
 * Each referenced port as written and observed (Phase 6 design §9.4): route port → Service port → target → slice ports.
 * It describes and does not judge.
 */
export function PortChain({ chains, stylesOverride }: { chains: Chain[]; stylesOverride?: SerializedStyles }) {
  return (
    <Section aria-labelledby="backend-ports">
      <h2 id="backend-ports">{portChainCopy.title}</h2>
      <Chains stylesOverride={stylesOverride}>
        {chains.map((c) => {
          const target = targetText(c);
          return (
            <Steps key={c.refPort} data-testid="port-chain">
              <Step label={portChainCopy.refPort} value={String(c.refPort)} />
              {c.servicePort !== undefined ? (
                <>
                  <Arrow />
                  <Step
                    label={portChainCopy.servicePort}
                    value={c.servicePortName ? `${c.servicePort} (${c.servicePortName})` : String(c.servicePort)}
                  />
                  {target ? (
                    <>
                      <Arrow />
                      <Step label={portChainCopy.targetPort} value={target} />
                    </>
                  ) : null}
                  <Arrow />
                  <Step
                    label={portChainCopy.slicePorts}
                    value={c.slicePorts.length ? c.slicePorts.join(", ") : portChainCopy.noneObserved}
                  />
                </>
              ) : null}
              {c.noSuchPort ? <span className="fact">{portChainCopy.noSuchPort(c.refPort)}</span> : null}
              {c.noSlicePort ? (
                <span className="fact">
                  {c.servicePortName ? portChainCopy.noSlicePort(c.servicePortName) : portChainCopy.noSlicePortUnnamed}
                </span>
              ) : null}
            </Steps>
          );
        })}
      </Chains>
    </Section>
  );
}
