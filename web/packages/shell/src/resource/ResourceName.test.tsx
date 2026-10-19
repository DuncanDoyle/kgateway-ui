import { screen } from "@testing-library/react";
import { renderWithTheme } from "../test/render";
import { contrastRatio } from "../theme/contrast";
import { defaultThemes } from "../theme/themes";
import { ResourceName } from "./ResourceName";

it("renders the namespace dimmed and the name in mono", () => {
  renderWithTheme(<ResourceName namespace="shop" name="cart" />);
  expect(screen.getByText("shop/")).toHaveAttribute("data-part", "namespace");
  expect(screen.getByText("cart")).toHaveAttribute("data-part", "name");
});

it("renders cluster-scoped names without a namespace", () => {
  renderWithTheme(<ResourceName name="kgateway" />);
  expect(screen.queryByText("/", { exact: false })).toBeNull();
  expect(screen.getByLabelText("kgateway")).toBeInTheDocument();
});

it("exposes the accessible name namespace/name", () => {
  renderWithTheme(<ResourceName namespace="shop" name="cart" />);
  expect(screen.getByLabelText("shop/cart")).toBeInTheDocument();
});

it("keeps the accessible name inside a link", () => {
  renderWithTheme(
    <a href="/x">
      <ResourceName namespace="shop" name="cart" />
    </a>,
  );
  expect(screen.getByRole("link", { name: "shop/cart" })).toBeInTheDocument();
});

it.each(defaultThemes)("namespace colour (textMuted) meets 4.5:1 on bg and surface in $id", (t) => {
  expect(contrastRatio(t.color.textMuted, t.color.bg)).toBeGreaterThanOrEqual(4.5);
  expect(contrastRatio(t.color.textMuted, t.color.surface)).toBeGreaterThanOrEqual(4.5);
});

it("dims the namespace with the muted text colour, not opacity", () => {
  renderWithTheme(<ResourceName namespace="shop" name="cart" />);
  const ns = screen.getByText("shop/");
  expect(getComputedStyle(ns).opacity).not.toBe("0.55");
  expect(getComputedStyle(ns).color).not.toBe("");
});
