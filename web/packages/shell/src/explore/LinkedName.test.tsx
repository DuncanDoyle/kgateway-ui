import { screen } from "@testing-library/react";
import type { LinkedRef } from "../api/types";
import { pseudoRules, renderWithTheme } from "../test/render";
import { controlRoomDark, controlRoomLight } from "../theme/themes";
import { LinkedName } from "./LinkedName";

const gw = "gateway.networking.k8s.io";
const ref: LinkedRef = { group: gw, kind: "Gateway", namespace: "infra", name: "edge", link: "detail" };

it("links a detail ref to its page, named namespace/name", () => {
  renderWithTheme(<LinkedName refValue={ref} installId="kgw.kgateway" />);
  expect(screen.getByRole("link", { name: "infra/edge" })).toHaveAttribute(
    "href",
    "/i/kgw.kgateway/gateways/infra/edge",
  );
});

it("links a yaml ref to the YAML view", () => {
  renderWithTheme(<LinkedName refValue={{ ...ref, kind: "TCPRoute", link: "yaml" }} installId="kgw.kgateway" />);
  expect(screen.getByRole("link", { name: "infra/edge" })).toHaveAttribute(
    "href",
    `/i/kgw.kgateway/r/${gw}/TCPRoute/infra/edge`,
  );
});

it("renders a none ref as text, never a link", () => {
  renderWithTheme(<LinkedName refValue={{ ...ref, link: "none" }} installId="kgw.kgateway" />);
  expect(screen.queryByRole("link")).toBeNull();
  expect(screen.getByLabelText("infra/edge")).toBeInTheDocument();
});

describe.each([controlRoomDark, controlRoomLight])("in the $name theme", (t) => {
  it("has hover, active and focus-visible styles", () => {
    renderWithTheme(<LinkedName refValue={ref} installId="kgw.kgateway" />, { themeId: t.id });
    const a = screen.getByRole("link");
    expect(pseudoRules(a, ':hover [data-part="name"]').join()).toContain("text-decoration:underline");
    expect(pseudoRules(a, ":active").join()).toContain(t.color.accentText);
    expect(pseudoRules(a, ":focus-visible").join()).toContain(t.color.focus);
  });
});
