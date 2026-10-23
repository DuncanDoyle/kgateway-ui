import { act, screen, waitFor } from "@testing-library/react";
import { renderWithTheme, stubApi } from "../test/render";
import { ApiError, apiFetch } from "./client";
import { useNow } from "./clock";
import { isLinkable, isStale, POLL_MS, useHealth, useInstalls, useOverview, useResource } from "./hooks";
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
  stubApi({ "/api/v1/installs": [{ id: "kgw.kgateway" }] });
  renderWithTheme(<Probe />);
  await waitFor(() => expect(screen.getByText("n=1")).toBeInTheDocument());
});

it("surfaces API errors with their status", async () => {
  stubApi({ "/api/v1/installs": [503, { error: "connecting to cluster: cache not synced" }] });
  renderWithTheme(<Probe />);
  await waitFor(() => expect(screen.getByText("error 503")).toBeInTheDocument());
});

it("after an outage of many polls the next retry is still one interval away, and fresh data shows", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  let calls = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      calls++;
      // Six failures (one poll, five retries), then the API is back.
      return calls <= 6
        ? new Response(JSON.stringify({ error: "down" }), { status: 503 })
        : new Response(JSON.stringify([{ id: "a.one" }, { id: "b.two" }]), { status: 200 });
    }),
  );
  renderWithTheme(<Probe />);
  await screen.findByText("error 503");
  expect(calls).toBe(1);
  for (let n = 2; n <= 7; n++) {
    // No backoff and no jitter: nothing before one interval, exactly one request at it.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS - 100);
    });
    expect(calls).toBe(n - 1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(calls).toBe(n);
  }
  expect(await screen.findByText("n=2")).toBeInTheDocument();
});

it("re-checks a 404 every interval and shows the object once it appears", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  let calls = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      calls++;
      return calls <= 3
        ? new Response(JSON.stringify({ error: "not found" }), { status: 404 })
        : new Response(JSON.stringify([{ id: "a.one" }]), { status: 200 });
    }),
  );
  renderWithTheme(<Probe />);
  await screen.findByText("error 404");
  for (let n = 2; n <= 4; n++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS);
    });
    expect(calls).toBe(n);
  }
  expect(await screen.findByText("n=1")).toBeInTheDocument();
});

it("does not retry a 400: a bad request stays bad for that URL", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  stubApi({ "/api/v1/installs": [400, { error: "bad query" }] });
  renderWithTheme(<Probe />);
  await screen.findByText("error 400");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3 * POLL_MS);
  });
  expect(vi.mocked(fetch)).toHaveBeenCalledTimes(1);
});

it("drops the previous install's data when the install changes", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/b.two")) await gate;
      const id = url.includes("/a.one") ? "a.one" : "b.two";
      return new Response(JSON.stringify({ header: { id } }), { status: 200 });
    }),
  );
  const { rerender } = renderWithTheme(<OverviewProbe id="a.one" />);
  await waitFor(() => expect(screen.getByText("overview a.one")).toBeInTheDocument());
  rerender(<OverviewProbe id="b.two" />);
  expect(screen.queryByText("overview a.one")).toBeNull();
  expect(screen.getByText("loading")).toBeInTheDocument();
  release?.();
  await waitFor(() => expect(screen.getByText("overview b.two")).toBeInTheDocument());
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
    platform: "Kubernetes",
    capabilities: ["scope", "workloads"],
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

it("treats an empty or unparsable collectedAt as stale", () => {
  const now = Date.parse("2026-10-17T12:00:00Z");
  expect(isStale("", now)).toBe(true);
  expect(isStale("not a date", now)).toBe(true);
});

it("normalises null checks to an empty list", async () => {
  stubApi({
    "/api/v1/installs/a.one/health": {
      install: "a.one",
      state: "healthy",
      findings: [],
      checks: null,
      counts: {},
      collectedAt: "2026-10-17T12:00:00Z",
    },
  });
  function HealthProbe() {
    const { data } = useHealth("a.one");
    return <p>{data ? `checks=${data.checks.length}` : "loading"}</p>;
  }
  renderWithTheme(<HealthProbe />);
  await waitFor(() => expect(screen.getByText("checks=0")).toBeInTheDocument());
});

it("wraps a non-JSON 2xx body as an ApiError", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("<html>", { status: 200 })),
  );
  await expect(apiFetch("/x")).rejects.toSatisfy((e: unknown) => e instanceof ApiError && e.status === 200);
});

it("does not re-render useResource consumers on a revalidation with unchanged data", async () => {
  stubApi({ "/api/v1/installs/a.one/resources/-/Service/shop/web": { kind: "Service" } });
  let renders = 0;
  let res: ReturnType<typeof useResource> | undefined;
  function ResourceProbe() {
    renders++;
    res = useResource("a.one", { group: "", kind: "Service", namespace: "shop", name: "web" });
    return <p>{res.data ? `kind=${String(res.data.kind)}` : "loading"}</p>;
  }
  renderWithTheme(<ResourceProbe />);
  await waitFor(() => expect(screen.getByText("kind=Service")).toBeInTheDocument());
  expect(Object.keys(res ?? {}).sort()).toEqual(["collectedAt", "data", "error", "mutate"]);
  const before = renders;
  await act(async () => {
    await res?.mutate();
  });
  expect(vi.mocked(fetch)).toHaveBeenCalledTimes(2);
  expect(renders).toBe(before);
});
