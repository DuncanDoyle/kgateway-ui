import { act, screen, waitFor } from "@testing-library/react";
import { renderWithTheme, stubApi } from "../test/render";
import { useNow } from "./clock";
import { isLinkable, isStale, POLL_MS, useInstalls, useOverview } from "./hooks";
import type { MetaResponse } from "./types";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function Probe() {
  const { data, error } = useInstalls();
  if (error) return <p>error {error.status}</p>;
  return <p>{data ? `n=${data.length}` : "loading"}</p>;
}

function OverviewProbe({ id }: { id: string }) {
  const { data } = useOverview(id);
  return <p>{data ? `overview ${data.header.id}` : "loading"}</p>;
}

it("loads installs", async () => {
  stubApi({ "/api/v1/installs": [{ id: "kgw/kgateway" }] });
  renderWithTheme(<Probe />);
  await waitFor(() => expect(screen.getByText("n=1")).toBeInTheDocument());
});

it("surfaces API errors with their status", async () => {
  stubApi({ "/api/v1/installs": [503, { error: "connecting to cluster: cache not synced" }] });
  renderWithTheme(<Probe />);
  await waitFor(() => expect(screen.getByText("error 503")).toBeInTheDocument());
});

it("drops the previous install's data when the install changes", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/b/two")) await gate;
      const id = url.includes("/a/one") ? "a/one" : "b/two";
      return new Response(JSON.stringify({ header: { id } }), { status: 200 });
    }),
  );
  const { rerender } = renderWithTheme(<OverviewProbe id="a/one" />);
  await waitFor(() => expect(screen.getByText("overview a/one")).toBeInTheDocument());
  rerender(<OverviewProbe id="b/two" />);
  expect(screen.queryByText("overview a/one")).toBeNull();
  expect(screen.getByText("loading")).toBeInTheDocument();
  release?.();
  await waitFor(() => expect(screen.getByText("overview b/two")).toBeInTheDocument());
});

it("marks data older than two poll intervals as stale", () => {
  const now = Date.parse("2026-10-17T12:00:00Z");
  expect(isStale(new Date(now - POLL_MS).toISOString(), now)).toBe(false);
  expect(isStale(new Date(now - 2 * POLL_MS - 1).toISOString(), now)).toBe(true);
});

it("useNow ticks every second from one shared clock", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(Date.parse("2026-10-17T12:00:00Z"));
  function Clock() {
    return <p>{new Date(useNow()).toISOString()}</p>;
  }
  renderWithTheme(
    <>
      <Clock />
      <Clock />
    </>,
  );
  expect(screen.getAllByText("2026-10-17T12:00:00.000Z")).toHaveLength(2);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000);
  });
  expect(screen.getAllByText("2026-10-17T12:00:03.000Z")).toHaveLength(2);
});

it("links only kinds that /api/v1/meta lists", () => {
  const meta: MetaResponse = {
    version: "dev",
    synced: true,
    kinds: [
      {
        group: "apps",
        kind: "Deployment",
        resource: "deployments",
        versions: ["v1"],
        namespaced: true,
        category: "workload",
        displayName: "Deployments",
        version: "v1",
      },
    ],
  };
  expect(isLinkable(meta, { group: "apps", kind: "Deployment", namespace: "kgw", name: "kgateway" })).toBe(true);
  expect(isLinkable(meta, { group: "", kind: "Secret", namespace: "kgw", name: "x" })).toBe(false);
  expect(isLinkable(undefined, { group: "apps", kind: "Deployment", name: "kgateway" })).toBe(false);
});
