import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import diagnosticsGolden from "../../../../../pkg/api/testdata/golden/diagnostics.json";
import findingDetailProcessGolden from "../../../../../pkg/api/testdata/golden/finding-detail-process.json";
import findingDetailProxyStatsGolden from "../../../../../pkg/api/testdata/golden/finding-detail-proxy-stats.json";
import findingDetailRuleReplacedGolden from "../../../../../pkg/api/testdata/golden/finding-detail-rule-replaced.json";
import findingDetailVersionSkewGolden from "../../../../../pkg/api/testdata/golden/finding-detail-version-skew.json";
import findingDetailXdsRejectsGolden from "../../../../../pkg/api/testdata/golden/finding-detail-xds-rejects.json";
import processesGolden from "../../../../../pkg/api/testdata/golden/processes.json";
import processesRestrictedGolden from "../../../../../pkg/api/testdata/golden/processes-restricted.json";
import { useDiagnostics, useProcesses } from "../api/diagHooks";
import type {
  Evidence,
  Finding,
  GroupRow as GroupRowData,
  NextStep,
  ProcessSet,
  SuggestedFix as SuggestedFixData,
} from "../api/types";
import { diagnosticsCopy, findingsCopy, fixCopy } from "../copy/explorer";
import { type FindingRenderer, ShellExtensionContext, useShellExtension } from "../shell/extension";
import { renderWhenFontsReady } from "../shell/renderWhenFontsReady";
import { pseudoRules, renderWithTheme, stubApi } from "../test/render";
import { FONT_FALLBACK_CLASS } from "../theme/fontFallback";
import { controlRoomDark, controlRoomLight } from "../theme/themes";
import { EvidenceView, findingDocUrl, LearnMore } from "./EvidenceView";
import { DiagnosticsGroupRow, GROUP_ROW_H } from "./GroupRow";
import { NextSteps } from "./NextSteps";
import { ProcessLine } from "./ProcessLine";
import { ProcessTable } from "./ProcessTable";
import { SuggestedFixView } from "./SuggestedFix";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const gw = "gateway.networking.k8s.io";
const step = (command: string, extra: Partial<NextStep> = {}): NextStep => ({
  title: "See the reason in the controller log",
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
    const button = screen.getByRole("button", { name: findingsCopy.copyLabel("See the reason in the controller log") });
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

describe("SuggestedFix", () => {
  const grant: SuggestedFixData = {
    title: "Create a ReferenceGrant in `store`",
    target: {
      group: gw,
      kind: "ReferenceGrant",
      namespace: "store",
      name: "allow-shop-httproute-service-cart",
      link: "none",
    },
    create: true,
    yaml: "apiVersion: gateway.networking.k8s.io/v1beta1\nkind: ReferenceGrant\nmetadata:\n  name: allow-shop-httproute-service-cart\n",
    basis: "Gateway API: a ref to another namespace needs a ReferenceGrant in that namespace.",
  };
  const fragment: SuggestedFixData = {
    title: "Set the group to `gateway.kgateway.dev`",
    target: { group: gw, kind: "HTTPRoute", namespace: "shop", name: "r", link: "detail" },
    create: false,
    path: "spec.rules[0].backendRefs[0]",
    yaml: "group: gateway.kgateway.dev\nkind: Backend\nname: cart\nnamespace: store\n",
    basis: "API discovery: only group `gateway.kgateway.dev` serves kind `Backend`.",
  };
  const show = (fix: SuggestedFixData) =>
    renderWithTheme(<SuggestedFixView fix={fix} installId="kgw.kgateway" />, { path: "/i/kgw.kgateway" });

  it("SuggestedFix copies and downloads the YAML", async () => {
    const writeText = clipboard();
    const blobs: Blob[] = [];
    // jsdom has no object URLs; the stub keeps the Blob the button hands over.
    Object.assign(URL, {
      createObjectURL: vi.fn((b: Blob) => {
        blobs.push(b);
        return "blob:fix";
      }),
      revokeObjectURL: vi.fn(),
    });
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    show(grant);
    expect(screen.getByRole("heading", { name: findingsCopy.suggestedFix })).toBeInTheDocument();
    expect(screen.getByText("store", { selector: "code" })).toBeInTheDocument();
    expect(screen.getByText(fixCopy.newObject)).toBeInTheDocument();
    const file = "referencegrant-allow-shop-httproute-service-cart.yaml";
    // A new object is created, never applied, so a taken name fails instead of replacing a grant (R89).
    expect(screen.getByText(`kubectl create -f ${file}`)).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/kubectl apply/);
    // The label reads the title without the backticks that mark names.
    const copy = screen.getByRole("button", { name: "Copy YAML: Create a ReferenceGrant in store" });
    expect(copy.getAttribute("aria-label")).not.toContain("`");
    const download = screen.getByRole("button", { name: fixCopy.downloadLabel(file) });
    expect(styled(copy)).toBe(true);
    expect(styled(download)).toBe(true);
    fireEvent.click(copy);
    expect(writeText).toHaveBeenCalledWith(grant.yaml);
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent(diagnosticsCopy.copied));
    fireEvent.click(download);
    expect(click).toHaveBeenCalledTimes(1);
    const a = click.mock.contexts[0] as HTMLAnchorElement;
    expect(a.download).toBe(file);
    const text = await new Promise<string>((done) => {
      const r = new FileReader();
      r.onload = () => done(String(r.result));
      r.readAsText(blobs[0] as Blob);
    });
    expect(text).toBe(grant.yaml);
    expect(screen.getByText(grant.yaml.split("\n")[0] as string, { exact: false }).tagName).toBe("PRE");
  });

  it("fragment shows its location", () => {
    show(fragment);
    expect(screen.getByText("spec.rules[0].backendRefs[0]")).toBeInTheDocument();
    expect(screen.getByText(fixCopy.merge)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /shop\/r|^r$/ })).toBeInTheDocument();
    expect(screen.queryByText(fixCopy.newObject)).toBeNull();
    expect(screen.queryByText(/kubectl create/)).toBeNull();
    expect(screen.getByRole("button", { name: fixCopy.downloadLabel("httproute-r-fix.yaml") })).toBeInTheDocument();
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

  const svc = (namespace: string, name: string) => ({ group: "", kind: "Service", namespace, name });

  it("renders ref-unresolved: a kind the group does not serve, attributed to one ref", () => {
    show({
      kind: "refs",
      explanation: "kind-not-served",
      attributed: true,
      candidates: [
        {
          path: "spec.rules[2].backendRefs[0]",
          ref: { group: "delegation.gateway.example", kind: "Service", namespace: "shop", name: "cart" },
        },
      ],
    });
    expect(screen.getByText(diagnosticsCopy.kindNotServed)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: diagnosticsCopy.refsAttributed })).toBeInTheDocument();
    const table = screen.getByRole("table", { name: diagnosticsCopy.refsAttributed });
    expect(within(table).getByText("spec.rules[2].backendRefs[0]")).toBeInTheDocument();
    expect(within(table).getByText("delegation.gateway.example/Service shop/cart")).toBeInTheDocument();
    expect(screen.queryByText(diagnosticsCopy.refsNotAttributed)).toBeNull();
  });

  it("renders ref-not-permitted: every candidate with its location, not attributed", () => {
    show({
      kind: "refs",
      explanation: "controller",
      attributed: false,
      candidates: [
        { path: "spec.rules[0].backendRefs[0]", ref: svc("store", "cart") },
        { path: "spec.rules[0].backendRefs[1]", ref: svc("bank", "pay") },
      ],
    });
    const table = screen.getByRole("table", { name: diagnosticsCopy.refsCandidates });
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    expect(within(table).getByText("Service store/cart")).toBeInTheDocument();
    expect(within(table).getByText("spec.rules[0].backendRefs[1]")).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.refsNotAttributed)).toBeInTheDocument();
    expect(screen.queryByText(diagnosticsCopy.kindNotServed)).toBeNull();
  });

  it("renders rule-replaced: the match index, reason, message and policy link", () => {
    show(findingDetailRuleReplacedGolden.body.evidence as Evidence);
    expect(screen.getByText(diagnosticsCopy.matchIndex(2))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.matchNote)).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.ruleReason.RouteRuleReplaced as string)).toBeInTheDocument();
    expect(screen.getByText(/ext auth unavailable/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /auth/ })).toBeInTheDocument();
  });

  it("renders rule-replaced without a parsed match: the message only", () => {
    show({ kind: "rule-replaced", reason: "RouteRuleDropped", message: "Dropped Rule: unparsed" });
    expect(screen.getByText(diagnosticsCopy.ruleReason.RouteRuleDropped as string)).toBeInTheDocument();
    expect(screen.getByText("Dropped Rule: unparsed")).toBeInTheDocument();
    expect(screen.queryByText(diagnosticsCopy.match)).toBeNull();
    expect(screen.queryByText(diagnosticsCopy.policy)).toBeNull();
  });

  it("renders freshness: the kind, the generations and since", () => {
    vi.useFakeTimers({ now: new Date("2026-10-17T12:00:00Z"), toFake: ["Date"] });
    show({ kind: "freshness", freshness: "stale", observed: 3, current: 4, since: "2026-10-17T11:55:00Z" });
    expect(screen.getByText(diagnosticsCopy.freshness.stale as string)).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.generations(3, 4))).toBeInTheDocument();
    expect(screen.getByText("5m ago")).toBeInTheDocument();
  });

  it("renders freshness: never written has no generations", () => {
    show({ kind: "freshness", freshness: "never-written" });
    expect(screen.getByText(diagnosticsCopy.freshness["never-written"] as string)).toBeInTheDocument();
    expect(screen.queryByText(diagnosticsCopy.generation)).toBeNull();
  });

  describe("endpoints", () => {
    const svc = { group: "", kind: "Service", namespace: "shop", name: "cart", link: "detail" } as const;
    const finding = {
      id: "f1",
      code: "KGWUI-023",
      slug: "backend-no-ready-endpoints",
      severity: "warning",
      resource: svc,
    };
    const ev: Evidence = {
      kind: "endpoints",
      service: "cart",
      port: 8080,
      portName: "http",
      ready: 0,
      total: 3,
      slices: 2,
    };

    it("renders the Service link, port with its name, ready of total and the slice count", () => {
      show(ev, finding as Finding);
      expect(screen.getByRole("link", { name: /cart/ })).toBeInTheDocument();
      expect(screen.getByText("8080 (http)")).toBeInTheDocument();
      expect(screen.getByText(diagnosticsCopy.readyEndpoints(0, 3))).toBeInTheDocument();
      expect(diagnosticsCopy.readyEndpoints(0, 0)).toBe("No endpoints");
      expect(screen.getByText(diagnosticsCopy.endpointSlices).nextElementSibling).toHaveTextContent("2");
    });

    it("an unnamed port shows the number alone, and a Service the viewer cannot open stays text", () => {
      show({ ...ev, portName: undefined } as Evidence, { ...finding, resource: { ...svc, link: "none" } } as Finding);
      expect(screen.getByText("8080")).toBeInTheDocument();
      expect(screen.queryByRole("link")).toBeNull();
      expect(screen.getByText(/cart/)).toBeInTheDocument();
    });

    it("without the finding it names the Service as text", () => {
      show(ev);
      expect(screen.getByText("cart")).toBeInTheDocument();
      expect(screen.queryByRole("link")).toBeNull();
    });

    describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
      it("the Service link has hover, active and focus-visible styles", () => {
        renderWithTheme(<EvidenceView evidence={ev} finding={finding as Finding} />, {
          path: "/i/kgw.kgateway",
          themeId: t.id,
        });
        const link = screen.getByRole("link", { name: /cart/ });
        expect(styled(link)).toBe(true);
        expect(pseudoRules(link, ":focus-visible").join()).toContain(t.color.focus);
        expect(pseudoRules(link, ":active").join()).toContain(t.color.accentText);
      });
    });
  });

  describe("withheld", () => {
    const withheld: Evidence = { kind: "withheld", withheld: 2, reportedBy: ["kgateway-0", "kgateway-1"] };
    const released: Evidence = {
      kind: "withheld",
      releases: [
        { mode: "flip_release", count: 1 },
        { mode: "warm_release", count: 2 },
      ],
      reportedBy: ["kgateway-0"],
    };

    it("withheld proxies: the count and the reporting Pods, no release table", () => {
      show(withheld);
      expect(screen.getByText(diagnosticsCopy.withheldProxies).nextElementSibling).toHaveTextContent("2");
      expect(screen.getByText(diagnosticsCopy.reportedBy).nextElementSibling).toHaveTextContent(
        "kgateway-0, kgateway-1",
      );
      expect(screen.queryByRole("table")).toBeNull();
    });

    it("released config: one row per mode with its label, mode and count, no withheld count", () => {
      show(released);
      expect(screen.queryByText(diagnosticsCopy.withheldProxies)).toBeNull();
      const rows = within(screen.getByRole("table", { name: diagnosticsCopy.releasedRecently })).getAllByRole("row");
      expect(rows).toHaveLength(3);
      expect(rows[1]).toHaveTextContent(`${diagnosticsCopy.releaseModes.flip_release} flip_release1`);
      expect(rows[2]).toHaveTextContent(`${diagnosticsCopy.releaseModes.warm_release} warm_release2`);
    });

    it("both signals together, and a mode without a label shows the mode alone", () => {
      show({
        ...withheld,
        releases: [
          { mode: "carry_expired", count: 1 },
          { mode: "new_mode", count: 4 },
        ],
      } as Evidence);
      expect(screen.getByText(diagnosticsCopy.withheldProxies)).toBeInTheDocument();
      const rows = within(screen.getByRole("table")).getAllByRole("row");
      expect(rows[1]).toHaveTextContent(`${diagnosticsCopy.releaseModes.carry_expired} carry_expired1`);
      expect(rows[2]).toHaveTextContent(/^new_mode4$/);
    });

    describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
      it("renders without interactive elements, so it adds no hover, active or focus targets", () => {
        renderWithTheme(<EvidenceView evidence={{ ...withheld, ...released, withheld: 1 } as Evidence} />, {
          path: "/i/kgw.kgateway",
          themeId: t.id,
        });
        expect(screen.getByRole("table")).toBeInTheDocument();
        expect(screen.queryByRole("link")).toBeNull();
        expect(screen.queryByRole("button")).toBeNull();
      });
    });
  });

  it("renders listener-conflict: the listener, the computed rule and the listeners kept", () => {
    show({
      kind: "listener-conflict",
      listener: "https",
      port: 443,
      protocol: "HTTPS",
      hostname: "a.example.com",
      reason: "HostnameConflict",
      message: "Listener conflicts with another",
      rule: "listener port and hostname",
      others: [
        {
          owner: { group: gw, kind: "ListenerSet", namespace: "apps", name: "first", link: "detail" },
          name: "web",
          hostname: "a.example.com",
        },
      ],
      othersTotal: 3,
    });
    expect(screen.getByText("https")).toBeInTheDocument();
    expect(screen.getByText("443")).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.computedRule("listener port and hostname"))).toBeInTheDocument();
    const table = screen.getByRole("table", { name: diagnosticsCopy.keptListeners });
    expect(within(table).getByRole("link", { name: /first/ })).toBeInTheDocument();
    expect(within(table).getByText("web")).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.more(2))).toBeInTheDocument();
  });

  it("renders listener-conflict for a reason no rule covers: no rule, no table", () => {
    show({
      kind: "listener-conflict",
      listener: "l",
      port: 80,
      reason: "PortUnavailable",
      message: "m",
      othersTotal: 0,
    });
    expect(screen.getByText("PortUnavailable")).toBeInTheDocument();
    expect(screen.queryByText(/Computed by the UI/)).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("renders status-progress: stuck objects per kind and no status-sync count", () => {
    const ev: Evidence = {
      kind: "status-progress",
      kinds: [
        { freshness: "stale", objects: 4 },
        { freshness: "never-written", objects: 1 },
      ],
      oldest: "2026-10-17T11:00:00Z",
    };
    const { unmount } = show(ev);
    expect(screen.getByRole("heading", { name: diagnosticsCopy.stuckObjects })).toBeInTheDocument();
    const table = screen.getByRole("table", { name: diagnosticsCopy.stuckObjects });
    expect(within(table).getByText(diagnosticsCopy.freshness.stale as string)).toBeInTheDocument();
    expect(within(table).getByText("4")).toBeInTheDocument();
    expect(screen.queryByText(/status syncs/i)).toBeNull();
    unmount();
  });

  it("renders process from the controller log only: no Pod row, the panic and deployer errors", () => {
    vi.useFakeTimers({ now: new Date("2026-10-17T12:00:00Z"), toFake: ["Date"] });
    show({
      kind: "process",
      controllerLogs: "read",
      panic: {
        class: "nil-pointer",
        frame: "github.com/kgateway-dev/kgateway/v2/pkg/x.(*T).Run",
        readAt: "2026-10-17T11:59:00Z",
      },
      deployer: [
        {
          class: "render",
          object: { group: gw, kind: "Gateway", namespace: "infra", name: "edge" },
          last: "2026-10-17T11:58:00Z",
        },
      ],
    });
    expect(screen.queryByText(diagnosticsCopy.pod)).toBeNull();
    expect(document.querySelectorAll("dd:empty")).toHaveLength(0);
    expect(screen.getByText(diagnosticsCopy.logState.read as string)).toBeInTheDocument();
    expect(screen.getByText("nil-pointer")).toBeInTheDocument();
    expect(screen.getByText("github.com/kgateway-dev/kgateway/v2/pkg/x.(*T).Run")).toBeInTheDocument();
    // A trace has no time: the time shown is when the log was read, and says so.
    expect(screen.getByText(diagnosticsCopy.panicReadAt("1m ago"))).toBeInTheDocument();
    expect(screen.queryByText("1m ago")).toBeNull();
    expect(screen.getByRole("heading", { name: diagnosticsCopy.deployerErrors })).toBeInTheDocument();
    const table = screen.getByRole("table", { name: diagnosticsCopy.deployerErrors });
    expect(within(table).getByText("render")).toBeInTheDocument();
    expect(within(table).getByText("Gateway infra/edge")).toBeInTheDocument();
  });

  it("renders process: an unread controller log reads Not read", () => {
    show({ ...(findingDetailProcessGolden.body.evidence as Evidence) });
    expect(screen.getByText(diagnosticsCopy.controllerLog)).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.logState["not-read"] as string)).toBeInTheDocument();
  });

  it("log events show names and class, never text", () => {
    const secret = "AKIAFAKEKEY123456";
    // Fields a raw log line might carry; the view must render only names and classes.
    const ev = {
      ...(findingDetailXdsRejectsGolden.body.evidence as Evidence),
      controllerLogs: "read",
      logEvents: [
        {
          typeUrl: "type.googleapis.com/envoy.config.cluster.v3.Cluster",
          since: "2026-10-17T11:58:00Z",
          classes: ["invalid-cluster"],
          envoyResources: ["kube_shop_cart_80"],
          message: `secret ${secret}`,
          line: secret,
        },
      ],
    } as unknown as Evidence;
    show(ev);
    expect(screen.getByRole("heading", { name: diagnosticsCopy.logEvents })).toBeInTheDocument();
    const table = screen.getByRole("table", { name: diagnosticsCopy.logEvents });
    expect(within(table).getByText("Cluster")).toBeInTheDocument();
    expect(within(table).getByText("invalid-cluster")).toBeInTheDocument();
    expect(within(table).getByText("kube_shop_cart_80")).toBeInTheDocument();
    expect(document.body.innerHTML).not.toContain(secret);
  });

  it("xds-rejects with an unread controller log says Not read and lists no events", () => {
    show(findingDetailXdsRejectsGolden.body.evidence as Evidence);
    expect(screen.getByText(diagnosticsCopy.controllerLog)).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.logState["not-read"] as string)).toBeInTheDocument();
    expect(screen.queryByRole("table", { name: diagnosticsCopy.logEvents })).toBeNull();
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

  it("a restricted finding with a registered renderer renders no renderer", () => {
    const renderer = vi.fn<FindingRenderer>(() => <p>acme hidden</p>);
    function WithRenderer({ children }: { children: ReactNode }) {
      const ext = useShellExtension();
      return (
        <ShellExtensionContext.Provider value={{ ...ext, findingRenderers: { "ACME-": renderer } }}>
          {children}
        </ShellExtensionContext.Provider>
      );
    }
    const finding: Finding = {
      code: "ACME-007",
      slug: "quota",
      severity: "warning",
      install: "kgw.kgateway",
      message: "",
    };
    const { container } = renderWithTheme(
      <WithRenderer>
        <EvidenceView finding={{ ...finding, restricted: true }} />
        <EvidenceView evidence={{ kind: "acme-quota" }} finding={{ ...finding, restricted: true }} />
      </WithRenderer>,
    );
    expect(renderer).not.toHaveBeenCalled();
    expect(container.textContent).toBe("");
  });

  it("an unknown kind without a renderer shows nothing", () => {
    const { container } = renderWithTheme(<EvidenceView evidence={{ kind: "acme-quota" }} />);
    expect(container.textContent).toBe("");
  });
});

describe("LearnMore", () => {
  const REPO = "https://github.com/DuncanDoyle/snellius/blob";
  const meta = (version: string) => ({
    "/api/v1/meta": { version, synced: true, platform: "kubernetes", capabilities: [], kinds: [] },
  });

  it("builds the findings reference anchor from the code", () => {
    expect(findingDocUrl("KGWUI-014")).toBe(`${REPO}/main/docs/findings.md#kgwui-014`);
    expect(findingDocUrl("KGWUI-001")).toMatch(/#kgwui-001$/);
    // The reference covers only the UI's own codes, not a downstream prefix.
    expect(findingDocUrl("ACME-001")).toBeUndefined();
    expect(findingDocUrl("KGWUI-1")).toBeUndefined();
  });

  it("a release version links that tag's reference; dev and other versions link main", () => {
    expect(findingDocUrl("KGWUI-014", "v0.2.0")).toBe(`${REPO}/v0.2.0/docs/findings.md#kgwui-014`);
    expect(findingDocUrl("KGWUI-014", "v1.3.0-rc.1")).toBe(`${REPO}/v1.3.0-rc.1/docs/findings.md#kgwui-014`);
    expect(findingDocUrl("KGWUI-014", "dev")).toBe(`${REPO}/main/docs/findings.md#kgwui-014`);
    for (const v of ["0.2.0", "v0.2", "abc1234", "v0.2.0+build.5", ""])
      expect(findingDocUrl("KGWUI-014", v)).toBe(`${REPO}/main/docs/findings.md#kgwui-014`);
  });

  it("a downstream docs URL replaces the reference, whatever the version", () => {
    const base = "https://docs.acme.example/findings";
    expect(findingDocUrl("KGWUI-014", "v0.2.0", base)).toBe(`${base}#kgwui-014`);
    expect(findingDocUrl("ACME-001", "v0.2.0", base)).toBeUndefined();
  });

  it("links the tag of the version /meta reports", async () => {
    stubApi(meta("v0.2.0"));
    renderWithTheme(<LearnMore code="KGWUI-014" />);
    await waitFor(() =>
      expect(screen.getByRole("link", { name: diagnosticsCopy.learnMoreLabel("KGWUI-014") })).toHaveAttribute(
        "href",
        `${REPO}/v0.2.0/docs/findings.md#kgwui-014`,
      ),
    );
  });

  it("links the extension's docs URL when a downstream sets one", async () => {
    stubApi(meta("v9.9.9"));
    function WithDocs({ children }: { children: ReactNode }) {
      const ext = useShellExtension();
      return (
        <ShellExtensionContext.Provider value={{ ...ext, findingsDocUrl: "https://docs.acme.example/findings" }}>
          {children}
        </ShellExtensionContext.Provider>
      );
    }
    renderWithTheme(
      <WithDocs>
        <LearnMore code="KGWUI-014" />
      </WithDocs>,
    );
    expect(screen.getByRole("link", { name: diagnosticsCopy.learnMoreLabel("KGWUI-014") })).toHaveAttribute(
      "href",
      "https://docs.acme.example/findings#kgwui-014",
    );
  });

  it("a downstream code gets no link", () => {
    const { container } = renderWithTheme(<LearnMore code="ACME-001" />);
    expect(container.textContent).toBe("");
  });

  describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
    it("opens the code's entry in a new tab, with hover, active and focus-visible styles", () => {
      renderWithTheme(<LearnMore code="KGWUI-017" />, { themeId: t.id });
      const link = screen.getByRole("link", { name: diagnosticsCopy.learnMoreLabel("KGWUI-017") });
      expect(link).toHaveTextContent(diagnosticsCopy.learnMore);
      expect(link).toHaveAttribute("href", findingDocUrl("KGWUI-017"));
      expect(link).toHaveAttribute("target", "_blank");
      expect(link).toHaveAttribute("rel", "noopener noreferrer");
      expect(pseudoRules(link, ":hover").join()).toContain(t.color.text);
      expect(pseudoRules(link, ":active").join()).toContain(t.color.textMuted);
      expect(pseudoRules(link, ":focus-visible").join()).toContain(t.color.focus);
    });
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
    // The whole row opens the cause: one link, with the row's hover and active and the link's focus ring.
    const link = screen.getByRole("link", { name: "Proxies reject this Gateway's config, Gateway infra/edge" });
    expect(link).toHaveAttribute("href", "/i/kgw.kgateway/gateways/infra/edge");
    expect(screen.getAllByRole("link")).toHaveLength(1);
    expect(screen.getByLabelText("infra/edge")).toBeInTheDocument();
    const line = link.closest(".line") as Element;
    expect(pseudoRules(line, ":hover").length > 0 && pseudoRules(line, ":active").length > 0).toBe(true);
    expect(pseudoRules(link, ":focus-visible").join()).toContain("outline");
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

  it("two groups with the same title name their different causes in the link", () => {
    const cause = (name: string) => ({ group: "", kind: "Service", namespace: "shop", name, link: "backend" as const });
    const title = "This backend has no ready endpoints";
    renderWithTheme(
      <>
        <DiagnosticsGroupRow group={group({ id: "a", title, cause: cause("cart") })} installId="kgw.kgateway" />
        <DiagnosticsGroupRow group={group({ id: "b", title, cause: cause("pay") })} installId="kgw.kgateway" />
      </>,
    );
    expect(screen.getAllByRole("link").map((l) => l.getAttribute("aria-label"))).toEqual([
      `${title}, Service shop/cart`,
      `${title}, Service shop/pay`,
    ]);
  });

  it("a Not permitted group names its grant shape instead of a cause", () => {
    renderWithTheme(
      <DiagnosticsGroupRow
        group={group({
          code: "KGWUI-014",
          cause: undefined,
          grant: { fromNamespace: "shop", toNamespace: "store", toGroup: "", toKind: "Service" },
        })}
        installId="kgw.kgateway"
      />,
    );
    expect(screen.getByText(diagnosticsCopy.grantShape("shop", "store", "", "Service"))).toBeInTheDocument();
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
      <ProcessLine
        kind="controller"
        summary={{ state: "healthy", observed: true, ready: 2, desired: 2, version: "2.4.7" }}
      />,
    );
    expect(screen.getByText(diagnosticsCopy.controller)).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.readyOf(2, 2))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.version("2.4.7"))).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
  });

  it("shows the state the server sends, never one of its own", () => {
    const { unmount } = renderWithTheme(
      <ProcessLine kind="controller" summary={{ state: "error", observed: true, ready: 0, desired: 0 }} />,
    );
    // 0/0 desired is KGWUI-005's error on the server: the line says so, never Healthy.
    expect(screen.getByText("Error")).toBeInTheDocument();
    expect(screen.queryByText("Healthy")).toBeNull();
    unmount();
    renderWithTheme(
      <ProcessLine
        kind="proxies"
        summary={{ state: "warning", gateways: 2, ready: 0, rejecting: 2, skew: 0, restarting: 0, notObserved: 0 }}
      />,
    );
    expect(screen.getByText("Warning")).toBeInTheDocument();
  });

  it("an unobserved controller reads Not observed, never failing", () => {
    renderWithTheme(<ProcessLine kind="controller" summary={{ state: "unknown", observed: false }} />);
    expect(screen.getByText(diagnosticsCopy.notObserved)).toBeInTheDocument();
    expect(screen.queryByText("Error")).toBeNull();
  });

  it("a restricted controller keeps its counts only", () => {
    renderWithTheme(
      <ProcessLine
        kind="controller"
        summary={{ state: "error", observed: true, ready: 1, desired: 2, restricted: true }}
      />,
    );
    expect(screen.getByText(diagnosticsCopy.readyOf(1, 2))).toBeInTheDocument();
    expect(screen.getByText(findingsCopy.restricted)).toBeInTheDocument();
  });

  it("the proxies line counts Gateways ready, rejecting, with skew, restarting and not observed", () => {
    renderWithTheme(
      <ProcessLine
        kind="proxies"
        summary={{ state: "error", gateways: 5, ready: 2, rejecting: 1, skew: 1, restarting: 1, notObserved: 1 }}
      />,
    );
    expect(screen.getByText(diagnosticsCopy.gatewaysReady(2, 5))).toBeInTheDocument();
    expect(screen.getByText(diagnosticsCopy.rejecting(1))).toBeInTheDocument();
    expect(screen.getByText("1 Gateway with version skew")).toBeInTheDocument();
    expect(screen.getByText("1 Gateway restarted recently")).toBeInTheDocument();
    expect(screen.getByText("1 Gateway not observed")).toBeInTheDocument();
    expect(screen.getByText("Error")).toBeInTheDocument();
  });

  it("the proxies line names Gateways in the plural", () => {
    renderWithTheme(
      <ProcessLine
        kind="proxies"
        summary={{ state: "error", gateways: 9, ready: 3, rejecting: 0, skew: 2, restarting: 3, notObserved: 4 }}
      />,
    );
    expect(screen.getByText("2 Gateways with version skew")).toBeInTheDocument();
    expect(screen.getByText("3 Gateways restarted recently")).toBeInTheDocument();
    expect(screen.getByText("4 Gateways not observed")).toBeInTheDocument();
  });

  it("the proxies line leaves self-managed Gateways out of the ready count", () => {
    renderWithTheme(
      <ProcessLine
        kind="proxies"
        summary={{
          state: "error",
          gateways: 6,
          ready: 4,
          rejecting: 4,
          skew: 0,
          restarting: 0,
          notObserved: 1,
          selfManaged: 1,
        }}
      />,
    );
    expect(screen.getByText("4 of 5 Gateways ready")).toBeInTheDocument();
    expect(screen.getByText("4 Gateways reject config")).toBeInTheDocument();
    expect(screen.getByText("1 self-managed Gateway")).toBeInTheDocument();
    expect(screen.queryByText(/not observed/)).toBeNull();
  });

  it("a proxies line of only self-managed Gateways shows no ready count", () => {
    renderWithTheme(
      <ProcessLine
        kind="proxies"
        summary={{
          state: "unknown",
          gateways: 2,
          ready: 0,
          rejecting: 0,
          skew: 0,
          restarting: 0,
          notObserved: 2,
          selfManaged: 2,
        }}
      />,
    );
    expect(screen.getByText(diagnosticsCopy.selfManaged(2))).toBeInTheDocument();
    expect(screen.queryByText(/Gateways ready/)).toBeNull();
    expect(screen.queryByText(/not observed/)).toBeNull();
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

  it("processes without probe facts show no probe column", () => {
    renderWithTheme(
      <ProcessTable
        set={{ observed: true, ready: 1, desired: 1, processes: [{ name: "edge-0", ready: true }] }}
        label="Proxies"
      />,
    );
    expect(screen.queryByRole("columnheader", { name: diagnosticsCopy.probe })).toBeNull();
    expect(screen.queryByText(/probe failed/i)).toBeNull();
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
    document.documentElement.classList.remove(FONT_FALLBACK_CLASS);
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
      // Fonts in time: the theme keeps its web fonts.
      expect(document.documentElement).not.toHaveClass(FONT_FALLBACK_CLASS);
    }
  });

  it("renders at the cap with the fallback fonts, which stay after a late font load", async () => {
    const { pending } = stubFonts();
    const { root, done } = await start("app");
    await act(async () => vi.advanceTimersByTime(2999));
    expect(root.textContent).toBe("");
    expect(document.documentElement).not.toHaveClass(FONT_FALLBACK_CLASS);
    await act(async () => {
      vi.advanceTimersByTime(1);
      await done;
    });
    expect(root.textContent).toBe("app");
    expect(document.documentElement).toHaveClass(FONT_FALLBACK_CLASS);
    await act(async () => {
      for (const p of pending) p.resolve();
    });
    expect(document.documentElement).toHaveClass(FONT_FALLBACK_CLASS);
  });
});
