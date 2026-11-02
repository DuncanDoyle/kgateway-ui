import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { renderWithTheme, stubApi } from "../test/render";
import { useFindingDetail, useFindings } from "./diagHooks";
import { POLL_MS } from "./hooks";
import type { FindingDetail, FindingList, FindingRow, LinkedRef } from "./types";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const gw = "gateway.networking.k8s.io";
const row = (id: string): FindingRow => ({ id, code: "KGWUI-002", slug: "condition-false", severity: "error" });
const list = (install: string, ...ids: string[]): FindingList => ({
  install,
  items: ids.map(row),
  total: ids.length,
  collectedAt: "2026-10-17T12:00:00Z",
});
const obj = (name: string): LinkedRef => ({ group: gw, kind: "HTTPRoute", namespace: "shop", name, link: "detail" });
const detail = (install: string, id: string, ...names: string[]): FindingDetail => ({
  install,
  finding: row(id),
  affected: { install, items: names.map(obj), total: names.length, collectedAt: "2026-10-17T12:00:00Z" },
  collectedAt: "2026-10-17T12:00:00Z",
});
const fetched = () => vi.mocked(fetch).mock.calls.map((c) => String(c[0]));

function FindingsProbe({ id, query = "" }: { id: string; query?: string }) {
  const { data, error, cursorRejected, isLaggy } = useFindings(id, new URLSearchParams(query));
  const ids = data?.items.map((f) => f.id).join(",");
  return (
    <p>
      {error && !cursorRejected ? `error ${error.status}` : data ? `rows ${ids}` : "loading"}
      {isLaggy ? " busy" : ""}
    </p>
  );
}

function DetailProbe({ id, findingId, cursor }: { id: string; findingId: string; cursor?: string }) {
  const { data, error, cursorRejected, isLaggy } = useFindingDetail(id, findingId, cursor);
  const names = data?.affected.items.map((r) => r.name).join(",");
  return (
    <p>
      {cursorRejected ? "cursor rejected" : error ? `error ${error.status}` : data ? `affected ${names}` : "loading"}
      {isLaggy ? " busy" : ""}
    </p>
  );
}

// renderWithTheme's rerender drops its SWR wrapper, so a probe's props change through state inside the tree.
function Switch<P>({ from, to, render }: { from: P; to: P; render: (p: P) => React.ReactNode }) {
  const [p, setP] = useState(from);
  return (
    <>
      <button type="button" onClick={() => setP(to)}>
        switch
      </button>
      {render(p)}
    </>
  );
}
const flip = () => fireEvent.click(screen.getByRole("button", { name: "switch" }));

it("loads findings with the page's query and polls", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  stubApi({ "/api/v1/installs/a.one/findings?severity=error&sort=code.desc": list("a.one", "f1-a", "f1-b") });
  renderWithTheme(<FindingsProbe id="a.one" query="severity=error&sort=code.desc" />);
  expect(await screen.findByText("rows f1-a,f1-b")).toBeInTheDocument();
  const before = fetched().length;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(POLL_MS + 100);
  });
  expect(fetched().length).toBeGreaterThan(before);
});

it("keeps the previous findings while a new query loads, marked busy", async () => {
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("severity=warning")) await gate;
      const body = url.includes("severity=warning") ? list("a.one", "f1-new") : list("a.one", "f1-old");
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  renderWithTheme(<Switch from="" to="severity=warning" render={(q) => <FindingsProbe id="a.one" query={q} />} />);
  expect(await screen.findByText("rows f1-old")).toBeInTheDocument();
  flip();
  expect(await screen.findByText("rows f1-old busy")).toBeInTheDocument();
  await act(async () => release?.());
  expect(await screen.findByText("rows f1-new")).toBeInTheDocument();
});

it("loads a finding with an encoded id and pages its affected objects", async () => {
  stubApi({
    "/api/v1/installs/a.one/findings/f1-a": detail("a.one", "f1-a", "one"),
    "/api/v1/installs/a.one/findings/f1-a?affectedCursor=n%2F2": detail("a.one", "f1-a", "two"),
  });
  let release: (() => void) | undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  const fetchStub = vi.mocked(fetch).getMockImplementation();
  vi.mocked(fetch).mockImplementation(async (url) => {
    if (String(url).includes("affectedCursor")) await gate;
    return (fetchStub as typeof fetch)(url);
  });
  renderWithTheme(
    <Switch<string | undefined>
      from={undefined}
      to="n/2"
      render={(c) => <DetailProbe id="a.one" findingId="f1-a" cursor={c} />}
    />,
  );
  expect(await screen.findByText("affected one")).toBeInTheDocument();
  flip();
  // The first page stays, marked busy, until the next one arrives.
  expect(await screen.findByText("affected one busy")).toBeInTheDocument();
  await act(async () => release?.());
  expect(await screen.findByText("affected two")).toBeInTheDocument();
});

it("reports a 400 on an affected cursor as rejected", async () => {
  stubApi({ "/api/v1/installs/a.one/findings/f1-a?affectedCursor=bad": [400, { error: "bad cursor" }] });
  renderWithTheme(<DetailProbe id="a.one" findingId="f1-a" cursor="bad" />);
  expect(await screen.findByText("cursor rejected")).toBeInTheDocument();
});

it("never shows another install's or another finding's detail", async () => {
  stubApi({
    "/api/v1/installs/b.two/findings/f1-a": detail("a.one", "f1-a", "one"),
    "/api/v1/installs/a.one/findings/f1-b": detail("a.one", "f1-a", "one"),
  });
  renderWithTheme(
    <Switch
      from={["b.two", "f1-a"]}
      to={["a.one", "f1-b"]}
      render={([id, f]) => <DetailProbe id={id as string} findingId={f as string} />}
    />,
  );
  await waitFor(() => expect(fetched()).toContain("/api/v1/installs/b.two/findings/f1-a"));
  expect(screen.getByText("loading")).toBeInTheDocument();
  flip();
  await waitFor(() => expect(fetched()).toContain("/api/v1/installs/a.one/findings/f1-b"));
  expect(screen.getByText("loading")).toBeInTheDocument();
});
