import { render, screen } from "@testing-library/react";
import { SWRConfig } from "swr";
import type { CorePages } from "../shell/extension";
import { Shell } from "../shell/Shell";
import { kubernetesVocabulary } from "../shell/vocabulary";
import { stubApi, testSWR } from "../test/render";
import { kgatewayWebPack } from "./webPack";

const pages: CorePages = {
  ClusterLanding: () => <p>cluster landing</p>,
  InstallPage: () => <p>install page</p>,
  FindingsPage: () => <p>findings page</p>,
  ResourcePage: () => <p>resource page</p>,
};
const meta = { version: "dev", synced: true, platform: "Kubernetes", capabilities: [], kinds: [] };

function mount(ext = {}) {
  window.history.pushState({}, "", "/");
  return render(
    <SWRConfig value={testSWR()}>
      <Shell extension={ext} pages={pages} />
    </SWRConfig>,
  );
}

afterEach(() => vi.unstubAllGlobals());

it("the core vocabulary names no implementation", () => {
  const { container } = render(
    <>
      <p>{kubernetesVocabulary.noInstallsTitle}</p>
      <p>{kubernetesVocabulary.noInstallsHelp}</p>
      <p>{kubernetesVocabulary.pinHint()}</p>
      <p>{kubernetesVocabulary.connecting}</p>
    </>,
  );
  expect(kubernetesVocabulary.noInstallsTitle).toBe("No Gateway API controller found");
  expect(container.textContent).not.toMatch(/kgateway/i);
});

it("without the kgateway web pack the empty state has no kgateway label hint", async () => {
  stubApi({ "/api/v1/installs": [], "/api/v1/meta": meta });
  mount();
  expect(await screen.findByText("No Gateway API controller found")).toBeInTheDocument();
  expect(document.body.textContent).not.toMatch(/kgateway/);
});

it("with the kgateway web pack the empty state keeps today's label hint", async () => {
  stubApi({ "/api/v1/installs": [], "/api/v1/meta": meta });
  mount(kgatewayWebPack);
  expect(await screen.findByText("No Gateway API controller found")).toBeInTheDocument();
  expect(screen.getByText("kgateway=kgateway")).toBeInTheDocument();
  expect(screen.getByText("app.kubernetes.io/component=controller")).toBeInTheDocument();
  expect(screen.getByText(/To always open one install, start the UI with/)).toBeInTheDocument();
});
