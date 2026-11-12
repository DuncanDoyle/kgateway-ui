import { ReactFlowProvider } from "@xyflow/react";
import type { ComponentProps } from "react";
import { StaleView } from "../../health/staleView";
import { renderWithTheme } from "../../test/render";
import type { HealthState } from "../../theme/tokens";
import { MapEdgeLine } from "./edges";

/** Stubs prefers-reduced-motion. */
const motion = (reduce: boolean) =>
  vi.stubGlobal(
    "matchMedia",
    vi.fn((q: string) => ({
      matches: reduce && q.includes("reduce"),
      media: q,
      addEventListener() {},
      removeEventListener() {},
    })),
  );

/** Draws one edge in state; returns whether it carries the flow overlay. */
function flows(state: HealthState, opts: { broken?: boolean; stale?: boolean } = {}) {
  const props = {
    id: "e",
    source: "a",
    target: "b",
    sourceX: 0,
    sourceY: 0,
    targetX: 100,
    targetY: 40,
    data: { state, broken: !!opts.broken, dim: false },
    style: { strokeWidth: 1.5 },
  } as unknown as ComponentProps<typeof MapEdgeLine>;
  const { container, unmount } = renderWithTheme(
    <StaleView.Provider value={!!opts.stale}>
      <ReactFlowProvider>
        <svg>
          <title>edge</title>
          <MapEdgeLine {...props} />
        </svg>
      </ReactFlowProvider>
    </StaleView.Provider>,
  );
  const on = container.querySelectorAll(".map-edge-flow").length;
  unmount();
  return on;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

it("only a healthy edge animates; warning, unknown, partly failing and broken edges never do", () => {
  motion(false);
  expect(flows("healthy")).toBe(1);
  expect(flows("warning")).toBe(0);
  expect(flows("unknown")).toBe(0);
  expect(flows("error")).toBe(0);
  expect(flows("error", { broken: true })).toBe(0);
});

it("under prefers-reduced-motion no edge animates", () => {
  motion(true);
  expect(flows("healthy")).toBe(0);
});

it("a stale view shows no animation", () => {
  motion(false);
  expect(flows("healthy", { stale: true })).toBe(0);
});
