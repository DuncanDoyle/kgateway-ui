import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import diagnosticsGolden from "../../../../../pkg/api/testdata/golden/diagnostics.json";
import findingDetailProcessGolden from "../../../../../pkg/api/testdata/golden/finding-detail-process.json";
import findingDetailProxyStatsGolden from "../../../../../pkg/api/testdata/golden/finding-detail-proxy-stats.json";
import findingDetailVersionSkewGolden from "../../../../../pkg/api/testdata/golden/finding-detail-version-skew.json";
import findingDetailXdsRejectsGolden from "../../../../../pkg/api/testdata/golden/finding-detail-xds-rejects.json";
import processesGolden from "../../../../../pkg/api/testdata/golden/processes.json";
import processesRestrictedGolden from "../../../../../pkg/api/testdata/golden/processes-restricted.json";
import { useDiagnostics, useProcesses } from "../api/diagHooks";
import type { Evidence, Finding, GroupRow as GroupRowData, NextStep, ProcessSet } from "../api/types";
import { diagnosticsCopy, findingsCopy } from "../copy/explorer";
import { type FindingRenderer, ShellExtensionContext, useShellExtension } from "../shell/extension";
import { renderWhenFontsReady } from "../shell/renderWhenFontsReady";
import { pseudoRules, renderWithTheme, stubApi } from "../test/render";
import { EvidenceView } from "./EvidenceView";
import { DiagnosticsGroupRow, GROUP_ROW_H } from "./GroupRow";
import { NextSteps } from "./NextSteps";
import { ProcessLine } from "./ProcessLine";
import { ProcessTable } from "./ProcessTable";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const gw = "gateway.networking.k8s.io";
const step = (command: string, extra: Partial<NextStep> = {}): NextStep => ({
  title: "See Envoy's reason",
  command,
  ...extra,
});
const clipboard = () => {
  const writeText = vi.fn(async () => {});
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
  return writeText;
};
/** Every interactive element carries hover, active and focus-visible rules. */
const styled = (el: Element) => [":hover", ":active", ":focus-visible"].every((p) => pseudoRules(el, p).length > 0);

describe("NextSteps", () => {
  it("NextSteps copies the command and announces it", async () => {
    const writeText = clipboard();
    renderWithTheme(
      <NextSteps steps={[step("kubectl logs -n kgw kgateway-0", { needs: ["pods/log"], note: "Share with care." })]} />,
    );
    const list = screen.getByRole("list");
    expect(within(list).getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText(findingsCopy.needs(["pods/log"]))).toBeInTheDocument();
    expect(screen.getByText("Share with care.")).toBeInTheDocument();
    const status = screen.getByRole("status");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(status).toHaveTextContent("");
    const button = screen.getByRole("button", { name: findingsCopy.copyLabel("See Envoy's reason") });
    expect(styled(button)).toBe(true);
    fireEvent.click(button);
    expect(writeText).toHaveBeenCalledWith("kubectl logs -n kgw kgateway-0");
    await waitFor(() => expect(status).toHaveTextContent(diagnosticsCopy.copied));
  });

  it("announces a failed copy", async () => {
    vi.stubGlobal("navigator", {
      ...navigator,
      clipboard: { writeText: vi.fn(async () => Promise.reject(new Error("no"))) },
    });
    renderWithTheme(<NextSteps steps={[step("kubectl get pods")]} />);
    fireEvent.click(screen.getByRole("button"));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent(diagnosticsCopy.copyFailed));
  });

  it("commands render as text, never HTML", () => {
    const command = `<img src=x onerror="alert(1)"> && echo <b>bold</b>`;
    const { container } = renderWithTheme(<NextSteps steps={[step(command, { note: "<i>note</i>" })]} />);
    expect(container.querySelector("img, b, i")).toBeNull();
    expect(container.querySelector("pre")?.textContent).toBe(command);
    expect(screen.getByText("<i>note</i>")).toBeInTheDocument();
  });
});

describe("EvidenceView", () => {
  const show = (evidence: Evidence, finding?: Finding) =>
    renderWithTheme(<EvidenceView evidence={evidence} finding={finding} />, { path: "/i/kgw.kgateway" });

  it("renders xds-rejects: rejected types, proxies, per-Pod stats and recent changes", () => {
    show(findingDetailXdsRejectsGolden.body.evidence as Evidence);
    const types = screen.getByRole("table", { name: diagnosticsCopy.rejectedTypes });
    expect(within(types).getByText("Cluster")).toBeInTheDocument();
    expect(within(types).getByText("RouteConfiguration")).toBeInTheDocument();
    expect(within(types).getAllByText("kgateway-0, kgateway-1")).toHaveLength(2);
    expect(screen.getByText(diagnosticsCopy.proxiesReady(2, 2))).toBeInTheDocument();
    const stats = screen.getByRole("table", { name: diagnosticsCopy.proxyRejects });
    expect(within(stats).getByText("listener~8080")).toBeInTheDocument();
    expect(within(stats).getAllByText("nack-0").length).toBeGreaterThan(0);
    expect(screen.getByText(diagnosticsCopy.recentlyChangedNote)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /fresh-n/ })).toBeInTheDocument();
    expect(screen.queryByText(diagnosticsCopy.statusSaysHealthy)).toBeNull();
  });

  it("renders xds-rejects with a contradicting status", () => {
    const ev = { ...(findingDetailXdsRejectsGolden.body.evidence as Evidence), statusSaysHealthy: true } as Evidence;
    show(ev);
    expect(screen.getByText(diagnosticsCopy.statusSaysHealthy)).toBeInTheDocument();
  });

  it("renders process: the Pod, restarts and how it last ended", () => {
    show(findingDetailProcessGolden.body.evidence as Evidence);
    expect(screen.getByText("kgateway-0")).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.termination("Error", 2))).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("renders process: waiting and probe failures with unknown history", () => {
    show({
      kind: "process",
      pod: "edge-0",
      waiting: "CrashLoopBackOff",
      probe: { failures: 4, last: "2026-10-17T11:59:00Z", historyUnknown: true },
    });
    expect(screen.getByText("CrashLoopBackOff")).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.probeFailures(undefined, 4))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.historyUnknown)).toBeInTheDocument();
  });

  it("renders version-skew: the controller and the Pods on another version", () => {
    show({
      ...(findingDetailVersionSkewGolden.body.evidence as Evidence),
      replicas: [{ version: "2.4.6", pods: ["kgateway-1"], total: 3 }],
      crds: [{ name: "httproutes.gateway.networking.k8s.io", bundleVersion: "v1.3.0", channel: "standard" }],
      expected: "v1.4",
    } as Evidence);
    expect(screen.getByText("2.4.7")).toBeInTheDocument();
    expect(screen.getByText("2.3.6")).toBeInTheDocument();
    expect(screen.getByText("edge-0")).toBeInTheDocument();
    expect(screen.getByText(`kgateway-1 ${diagnosticsCopy.more(2)}`)).toBeInTheDocument();
    expect(screen.getByText("httproutes.gateway.networking.k8s.io")).toBeInTheDocument();
    expect(screen.getByText("v1.3.0 (standard)")).toBeInTheDocument();
    expect(screen.getByText("v1.4")).toBeInTheDocument();
  });

  it("renders proxy-stats: one row per Pod, unreported fields muted", () => {
    show(findingDetailProxyStatsGolden.body.evidence as Evidence);
    const table = screen.getByRole("table", { name: diagnosticsCopy.proxyStats });
    const row = within(table).getByText("edge-0").closest("tr") as HTMLElement;
    expect(within(row).getByText(diagnosticsCopy.disconnected)).toBeInTheDocument();
    expect(within(row).getByText("live")).toBeInTheDocument();
    expect(within(row).getAllByText(diagnosticsCopy.notReported)).toHaveLength(2);
  });

  it("an unknown kind uses the finding renderer", () => {
    const renderer = vi.fn<FindingRenderer>((f) => <p>acme {f.code}</p>);
    function WithRenderer({ children }: { children: ReactNode }) {
      const ext = useShellExtension();
      return (
        <ShellExtensionContext.Provider value={{ ...ext, findingRenderers: { "ACME-": renderer } }}>
          {children}
        </ShellExtensionContext.Provider>
      );
    }
    const evidence = { kind: "acme-quota", used: 3 };
    const finding: Finding = {
      code: "ACME-007",
      slug: "quota",
      severity: "warning",
      install: "kgw.kgateway",
      message: "m",
    };
    renderWithTheme(
      <WithRenderer>
        <EvidenceView evidence={evidence} finding={finding} />
      </WithRenderer>,
    );
    expect(screen.getByText("acme ACME-007")).toBeInTheDocument();
    expect(renderer).toHaveBeenCalledWith(expect.objectContaining({ code: "ACME-007", evidence }));
  });

  it("an unknown kind without a renderer shows nothing", () => {
    const { container } = renderWithTheme(<EvidenceView evidence={{ kind: "acme-quota" }} />);
    expect(container.textContent).toBe("");
  });
});

describe("GroupRow", () => {
  const group = (extra: Partial<GroupRowData> = {}): GroupRowData => ({
    id: "f1-abc",
    title: "Proxies reject this Gateway's config",
    code: "KGWUI-001",
    severity: "error",
    class: 1,
    cause: { group: gw, kind: "Gateway", namespace: "infra", name: "edge", link: "detail" },
    members: 1,
    affected: [],
    affectedTotal: 37,
    since: new Date(Date.now() - 5 * 60_000).toISOString(),
    nextSteps: [step("kubectl logs -n kgw kgateway-0")],
    ...extra,
  });

  it("shows state, title, cause, blast radius, age and code, with a next-step expander", () => {
    renderWithTheme(<DiagnosticsGroupRow group={group()} installId="kgw.kgateway" />);
    expect(screen.getByText("Error")).toBeInTheDocument();
    expect(screen.getByText("Proxies reject this Gateway's config")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /edge/ })).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.affected(37))).toBeInTheDocument();
    expect(screen.getByText("5m ago")).toBeInTheDocument();
    expect(screen.getByText("KGWUI-001")).toBeInTheDocument();
    const toggle = screen.getByRole("button", { name: diagnosticsCopy.nextStep });
    expect(styled(toggle)).toBe(true);
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("kubectl logs -n kgw kgateway-0")).toBeNull();
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    const panel = document.getElementById(toggle.getAttribute("aria-controls") ?? "");
    expect(panel).toHaveTextContent("kubectl logs -n kgw kgateway-0");
  });

  it("a restricted group shows severity, code and the placeholder, never its cause", () => {
    const hidden = group({
      restricted: true,
      title: "",
      affectedTotal: 0,
      nextSteps: undefined,
      since: undefined,
      cause: { group: gw, kind: "Gateway", namespace: "infra", name: "secret-gw", link: "none" },
    });
    const { container } = renderWithTheme(<DiagnosticsGroupRow group={hidden} installId="kgw.kgateway" />);
    expect(screen.getByText(findingsCopy.restricted)).toBeInTheDocument();
    expect(screen.getByText("KGWUI-001")).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/secret-gw/);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("reserves a fixed row height, so late data shifts nothing", () => {
    const { container } = renderWithTheme(
      <DiagnosticsGroupRow group={group({ nextSteps: undefined })} installId="kgw.kgateway" />,
    );
    const row = container.querySelector("[data-testid=group-row]") as HTMLElement;
    const css = [...document.querySelectorAll("style")].map((s) => s.textContent ?? "").join("");
    const own = [...row.classList].some((c) => new RegExp(`\\.${c}[^{]*\\{[^}]*min-height:${GROUP_ROW_H}px`).test(css));
    expect(own).toBe(true);
  });
});

describe("ProcessLine", () => {
  it("the controller line shows ready over desired and the version", () => {
    renderWithTheme(
      <ProcessLine kind="controller" summary={{ observed: true, ready: 2, desired: 2, version: "2.4.7" }} />,
    );
    expect(screen.getByText(diagnosticsCopy.controller)).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.readyOf(2, 2))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.version("2.4.7"))).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
  });

  it("an unobserved controller reads Not observed, never failing", () => {
    renderWithTheme(<ProcessLine kind="controller" summary={{ observed: false }} />);
    expect(screen.getByText(diagnosticsCopy.notObserved)).toBeInTheDocument();
    expect(screen.queryByText("Error")).toBeNull();
  });

  it("a restricted controller keeps its counts only", () => {
    renderWithTheme(
      <ProcessLine kind="controller" summary={{ observed: true, ready: 1, desired: 2, restricted: true }} />,
    );
    expect(screen.getByText(diagnosticsCopy.readyOf(1, 2))).toBeInTheDocument();
    expect(screen.getByText(findingsCopy.restricted)).toBeInTheDocument();
  });

  it("the proxies line counts Gateways ready, rejecting, with skew, restarting and not observed", () => {
    renderWithTheme(
      <ProcessLine
        kind="proxies"
        summary={{ gateways: 5, ready: 2, rejecting: 1, skew: 1, restarting: 1, notObserved: 1 }}
      />,
    );
    expect(screen.getByText(diagnosticsCopy.gatewaysReady(2, 5))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.rejecting(1))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.skew(1))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.restarting(1))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.gatewaysNotObserved(1))).toBeInTheDocument();
    expect(screen.getByText("Error")).toBeInTheDocument();
  });
});

describe("ProcessTable", () => {
  it("lists each process with readiness, restarts, version and how it last ended", () => {
    const edge = processesGolden.body.proxies.items[1]?.set as ProcessSet;
    renderWithTheme(<ProcessTable set={edge} label="Proxies of infra/edge" />);
    const table = screen.getByRole("table", { name: "Proxies of infra/edge" });
    const row = within(table).getByText("edge-0").closest("tr") as HTMLElement;
    expect(within(row).getByText("Ready")).toBeInTheDocument();
    expect(within(row).getByText(diagnosticsCopy.restarts(1, 1))).toBeInTheDocument();
    expect(within(row).getByText("2.4.5")).toBeInTheDocument();
    expect(within(row).getByText(diagnosticsCopy.termination("Error", 1))).toBeInTheDocument();
  });

  it("an unobserved set reads Not observed", () => {
    renderWithTheme(<ProcessTable set={{ observed: false, processes: [] }} label="Proxies" />);
    expect(screen.getByText(diagnosticsCopy.notObserved)).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("a restricted set shows its counts and the placeholder, no processes", () => {
    renderWithTheme(<ProcessTable set={processesRestrictedGolden.body.controller as ProcessSet} label="Controller" />);
    expect(screen.getByText(findingsCopy.restricted)).toBeInTheDocument();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("a stuck rollout names its reason", () => {
    renderWithTheme(
      <ProcessTable
        set={{ observed: true, processes: [], rollout: { progressing: false, reason: "ProgressDeadlineExceeded" } }}
        label="Controller"
      />,
    );
    expect(screen.getByText(diagnosticsCopy.rolloutStuck("ProgressDeadlineExceeded"))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.noProcesses)).toBeInTheDocument();
  });
});

describe("hooks", () => {
  const api = "/api/v1/installs/kgw.kgateway";
  function Probe({ id }: { id: string }) {
    const d = useDiagnostics(id);
    const p = useProcesses(id, new URLSearchParams("skew=true"));
    return <p>{`groups ${d.data?.groupTotal ?? "-"}, gateways ${p.data?.proxies.total ?? "-"}`}</p>;
  }

  it("useDiagnostics and useProcesses read their install's endpoints, with the query", async () => {
    stubApi({ [`${api}/diagnostics`]: diagnosticsGolden.body, [`${api}/processes?skew=true`]: processesGolden.body });
    renderWithTheme(<Probe id="kgw.kgateway" />);
    expect(await screen.findByText(`groups ${diagnosticsGolden.body.groupTotal}, gateways 3`)).toBeInTheDocument();
  });

  it("never shows another install's response", async () => {
    stubApi({
      "/api/v1/installs/other/diagnostics": diagnosticsGolden.body,
      "/api/v1/installs/other/processes?skew=true": processesGolden.body,
    });
    renderWithTheme(<Probe id="other" />);
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalledTimes(2));
    await act(async () => {});
    expect(screen.getByText("groups -, gateways -")).toBeInTheDocument();
  });
});

describe("renderWhenFontsReady", () => {
  /** Stubs document.fonts; each load stays pending until settled by index. */
  function stubFonts() {
    const pending: { resolve: () => void; reject: () => void }[] = [];
    const load = vi.fn(
      () =>
        new Promise<FontFace[]>((resolve, reject) => {
          pending.push({ resolve: () => resolve([]), reject: () => reject(new Error("font")) });
        }),
    );
    Object.defineProperty(document, "fonts", { value: { load }, configurable: true });
    return { load, pending };
  }
  async function start(text: string) {
    const root = document.createElement("div");
    document.body.append(root);
    const done = renderWhenFontsReady(root, <p>{text}</p>, ["Figtree", "Mono"]);
    await act(async () => {});
    return { root, done };
  }
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  });
  afterEach(() => {
    Reflect.deleteProperty(document, "fonts");
    document.body.replaceChildren();
  });

  it("renderWhenFontsReady renders after fonts load and after a font failure", async () => {
    for (const fail of [false, true]) {
      const { load, pending } = stubFonts();
      const { root, done } = await start(`app ${fail}`);
      expect(load).toHaveBeenCalledTimes(2);
      // The first font settling is not enough: the render waits for every font, failed or loaded.
      await act(async () => (fail ? pending[0]?.reject() : pending[0]?.resolve()));
      expect(root.textContent).toBe("");
      await act(async () => {
        pending[1]?.resolve();
        await done;
      });
      expect(root.textContent).toBe(`app ${fail}`);
    }
  });

  it("renders at the cap when a font never settles", async () => {
    stubFonts();
    const { root, done } = await start("app");
    await act(async () => vi.advanceTimersByTime(2999));
    expect(root.textContent).toBe("");
    await act(async () => {
      vi.advanceTimersByTime(1);
      await done;
    });
    expect(root.textContent).toBe("app");
  });
});
