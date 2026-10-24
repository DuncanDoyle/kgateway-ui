import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Ref } from "../api/types";
import { ShellExtensionContext, useShellExtension } from "../shell/extension";
import { kubernetesVocabulary, type Vocabulary } from "../shell/vocabulary";
import { meta } from "../test/fixtures";
import { pseudoRules, renderWithTheme, stubApi } from "../test/render";
import { controlRoomDark, controlRoomLight } from "../theme/themes";
import { CopyCommandButton } from "./CopyCommandButton";

afterEach(() => {
  vi.unstubAllGlobals();
});

const route: Ref = { group: "gateway.networking.k8s.io", kind: "HTTPRoute", namespace: "shop", name: "api" };

function WithVocabulary({ vocabulary, children }: { vocabulary: Partial<Vocabulary>; children: ReactNode }) {
  const ext = useShellExtension();
  return (
    <ShellExtensionContext.Provider value={{ ...ext, vocabulary: { ...ext.vocabulary, ...vocabulary } }}>
      {children}
    </ShellExtensionContext.Provider>
  );
}

describe("the default getCommand", () => {
  const cmd = kubernetesVocabulary.getCommand;
  it("names the resource with its group and namespace", () => {
    expect(cmd(route, "httproutes")).toBe("kubectl get httproutes.gateway.networking.k8s.io -n shop api -o yaml");
  });
  it("drops the group for the core group", () => {
    expect(cmd({ group: "", kind: "Service", namespace: "shop", name: "cart" }, "services")).toBe(
      "kubectl get services -n shop cart -o yaml",
    );
  });
  it("drops -n for a cluster-scoped resource", () => {
    expect(cmd({ group: "gateway.networking.k8s.io", kind: "GatewayClass", name: "kgw" }, "gatewayclasses")).toBe(
      "kubectl get gatewayclasses.gateway.networking.k8s.io kgw -o yaml",
    );
  });
});

it("copies the command for the ref, with the resource from /meta", async () => {
  stubApi({ "/api/v1/meta": meta });
  const writeText = vi.fn(async () => {});
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
  renderWithTheme(<CopyCommandButton refValue={route} />);
  await userEvent.click(await screen.findByRole("button", { name: "Copy kubectl command" }));
  expect(writeText).toHaveBeenCalledWith("kubectl get httproutes.gateway.networking.k8s.io -n shop api -o yaml");
});

it("getCommand returning undefined hides the copy button", async () => {
  stubApi({ "/api/v1/meta": meta });
  const getCommand = vi.fn(() => undefined);
  renderWithTheme(
    <WithVocabulary vocabulary={{ getCommand }}>
      <CopyCommandButton refValue={route} />
    </WithVocabulary>,
  );
  await vi.waitFor(() => expect(getCommand).toHaveBeenCalled());
  expect(screen.queryByRole("button")).toBeNull();
});

it("hides the button for a kind /meta does not serve", async () => {
  stubApi({ "/api/v1/meta": meta });
  renderWithTheme(<CopyCommandButton refValue={{ ...route, kind: "Unserved" }} />);
  // The button never shows, even once meta has loaded.
  await new Promise((r) => setTimeout(r, 20));
  expect(screen.queryByRole("button")).toBeNull();
});

describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
  it("has hover, active and focus-visible styles", async () => {
    stubApi({ "/api/v1/meta": meta });
    renderWithTheme(<CopyCommandButton refValue={route} />, { themeId: t.id });
    const b = await screen.findByRole("button", { name: "Copy kubectl command" });
    expect(pseudoRules(b, ":hover").join()).toContain(t.color.borderStrong);
    expect(pseudoRules(b, ":active").join()).toContain(t.color.surfaceRaised);
    expect(pseudoRules(b, ":focus-visible").join()).toContain(t.color.focus);
  });
});
