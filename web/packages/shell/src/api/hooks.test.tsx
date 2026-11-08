import { act, screen, waitFor } from "@testing-library/react";
import { DataAge } from "../pages/DataAge";
import { renderWithTheme, stubApi } from "../test/render";
import { ApiError, apiFetch } from "./client";
import { resetClockForTests, useNow } from "./clock";
import {
  isLinkable,
  isStale,
  POLL_MS,
  supportsFilter,
  useCapabilityState,
  useHealth,
  useInstalls,
  useListFilter,
  useOverview,
  useResource,
} from "./hooks";
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

it("supportsFilter reads the post-v1 filters /meta advertises", () => {
  const meta = { filters: { routes: ["listener"] } } as unknown as MetaResponse;
  expect(supportsFilter(meta, "routes", "listener")).toBe(true);
  expect(supportsFilter(meta, "routes", "backend")).toBe(false);
  expect(supportsFilter(meta, "gateways", "listener")).toBe(false);
  // A 2.2 server sends no filters object; nothing post-v1 is supported.
  expect(supportsFilter({} as MetaResponse, "routes", "listener")).toBe(false);
  expect(supportsFilter(undefined, "routes", "listener")).toBe(false);
});

function FilterProbe() {
  const ok = useListFilter("routes", "listener");
  return <p>{ok === undefined ? "loading" : `listener ${ok}`}</p>;
}

it("useListFilter is undefined until /meta loads, then says whether the filter is advertised", async () => {
  stubApi({ "/api/v1/meta": { kinds: [], capabilities: [], filters: { routes: ["listener"] } } });
  const r = renderWithTheme(<FilterProbe />);
  expect(screen.getByText("loading")).toBeInTheDocument();
  await screen.findByText("listener true");
  r.unmount();
  stubApi({ "/api/v1/meta": { kinds: [], capabilities: [] } });
  renderWithTheme(<FilterProbe />);
  await screen.findByText("listener false");
});

function CapabilityProbe({ name }: { name: string }) {
  const c = useCapabilityState(name);
  return <p>{c ? `${c.name} ${c.state} ${c.reason ?? ""}`.trim() : "none"}</p>;
}

it("useCapabilityState reads capabilityStates", async () => {
  stubApi({
    "/api/v1/meta": {
      kinds: [],
      capabilities: ["acme-widgets"],
      capabilityStates: [
        { name: "acme-audit", state: "denied", reason: "no access" },
        { name: "acme-widgets", state: "on" },
      ],
    },
  });
  const r = renderWithTheme(
    <>
      <CapabilityProbe name="acme-audit" />
      <CapabilityProbe name="acme-widgets" />
      <CapabilityProbe name="acme-absent" />
    </>,
  );
  expect(await screen.findByText("acme-audit denied no access")).toBeInTheDocument();
  expect(screen.getByText("acme-widgets on")).toBeInTheDocument();
  expect(screen.getByText("none")).toBeInTheDocument();
  r.unmount();
  // A server without extension capabilities omits the field.
  stubApi({ "/api/v1/meta": { kinds: [], capabilities: [] } });
  renderWithTheme(<CapabilityProbe name="acme-widgets" />);
  await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled());
  expect(screen.getByText("none")).toBeInTheDocument();
});

// region Hidden tab (D-063)
function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
  document.dispatchEvent(new Event("visibilitychange"));
}

function AgeProbe() {
  const { data } = useInstalls();
  return data?.[0] ? <DataAge collectedAt={data[0].computedAt} /> : <p>loading</p>;
}

/** Serves installs collected at `collectedAt()` (now by default); the request after `hold()` waits until released. */
function gatedInstalls(collectedAt: () => string = () => new Date().toISOString()) {
  let gate: Promise<void> | undefined;
  let open = () => {};
  let fail = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      if (gate) await gate;
      if (fail) return new Response(JSON.stringify({ error: "down" }), { status: 503 });
      return new Response(JSON.stringify([{ id: "a.one", computedAt: collectedAt() }]), { status: 200 });
    }),
  );
  return {
    hold(failing = false) {
      fail = failing;
      gate = new Promise<void>((r) => {
        open = r;
      });
    },
    release: () => open(),
  };
}

async function hideFor(ms: number) {
  act(() => setVisibility("hidden"));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe("after a hidden tab is shown again", () => {
  afterEach(() => {
    act(() => setVisibility("visible"));
    resetClockForTests();
  });

  it("data stays fresh until the first poll answers", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const api = gatedInstalls();
    renderWithTheme(<AgeProbe />);
    expect(await screen.findByText(/^Last checked/)).toBeInTheDocument();
    await hideFor(9 * 60_000);
    api.hold();
    const before = vi.mocked(fetch).mock.calls.length;
    act(() => setVisibility("visible"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    // The revalidation is in flight; the 9 minutes hidden do not count yet.
    expect(vi.mocked(fetch).mock.calls.length).toBeGreaterThan(before);
    expect(screen.queryByText(/stale/)).toBeNull();
    api.release();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(screen.getByText(/^Last checked/)).toBeInTheDocument();
    expect(screen.queryByText(/stale/)).toBeNull();
  });

  it("a failing first poll shows the data as stale", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const api = gatedInstalls();
    renderWithTheme(<AgeProbe />);
    await screen.findByText(/^Last checked/);
    await hideFor(9 * 60_000);
    api.hold(true);
    act(() => setVisibility("visible"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(screen.queryByText(/stale/)).toBeNull();
    api.release();
    expect(await screen.findByText(/^Data is stale\. Last checked 9m ago/)).toBeInTheDocument();
  });

  it("a first poll with data older than the stale limit shows it as stale", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const t0 = new Date().toISOString();
    const api = gatedInstalls(() => t0);
    renderWithTheme(<AgeProbe />);
    await screen.findByText(/^Last checked/);
    await hideFor(9 * 60_000);
    api.hold();
    act(() => setVisibility("visible"));
    api.release();
    expect(await screen.findByText(/^Data is stale/)).toBeInTheDocument();
  });

  it("a first poll that never answers shows the data as stale within 10 s", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const api = gatedInstalls();
    renderWithTheme(<AgeProbe />);
    await screen.findByText(/^Last checked/);
    await hideFor(9 * 60_000);
    api.hold();
    act(() => setVisibility("visible"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(9000);
    });
    expect(screen.queryByText(/stale/)).toBeNull();
    // The revalidation stays pending; the hold cap alone releases the clock.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(screen.getByText(/^Data is stale\. Last checked 9m ago/)).toBeInTheDocument();
  });

  it("a visible tab whose backend stops answering still turns stale after two intervals", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const api = gatedInstalls();
    renderWithTheme(<AgeProbe />);
    await screen.findByText(/^Last checked/);
    api.hold(true);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2 * POLL_MS + 1000);
    });
    expect(screen.getByText(/^Data is stale/)).toBeInTheDocument();
  });
});

// region Clock skew (4.3)
const SKEW = 35 * 60_000;

/** Serves installs collected now on a server clock skew() ms off the browser's; after fail() the backend is unreachable. */
function skewedInstalls(skew: () => number) {
  let down = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      if (down) throw new TypeError("Failed to fetch");
      const at = Date.now() + skew();
      return new Response(JSON.stringify([{ id: "a.one", computedAt: new Date(at).toISOString() }]), {
        status: 200,
        headers: { Date: new Date(at).toUTCString() },
      });
    }),
  );
  return { fail: () => (down = true) };
}

describe("data age on the server's clock", () => {
  afterEach(() => {
    act(() => setVisibility("visible"));
    resetClockForTests();
  });

  it("a server clock 35 min behind keeps fresh data fresh", async () => {
    skewedInstalls(() => -SKEW);
    renderWithTheme(<AgeProbe />);
    expect(await screen.findByText("Last checked 0s ago")).toBeInTheDocument();
    expect(screen.queryByText(/stale/)).toBeNull();
  });

  it("a server clock 35 min ahead still turns stale when polls fail", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const api = skewedInstalls(() => SKEW);
    renderWithTheme(<AgeProbe />);
    await screen.findByText(/^Last checked/);
    api.fail();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2 * POLL_MS + 1000);
    });
    expect(screen.getByText(/^Data is stale\. Last checked 1[0-2]s ago/)).toBeInTheDocument();
  });

  it("equal clocks read as before: a Date header cut to the second adds no skew", async () => {
    skewedInstalls(() => 0);
    renderWithTheme(<AgeProbe />);
    expect(await screen.findByText("Last checked 0s ago")).toBeInTheDocument();
  });

  it("a server clock that fell 35 min behind while the tab was hidden does not read as stale once shown", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let skew = 0;
    skewedInstalls(() => skew);
    renderWithTheme(<AgeProbe />);
    await screen.findByText(/^Last checked/);
    await hideFor(9 * 60_000);
    skew = -SKEW;
    act(() => setVisibility("visible"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(screen.getByText(/^Last checked [0-2]s ago/)).toBeInTheDocument();
    expect(screen.queryByText(/stale/)).toBeNull();
  });
});
