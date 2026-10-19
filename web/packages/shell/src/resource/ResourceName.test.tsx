import { screen } from "@testing-library/react";
import { renderWithTheme } from "../test/render";
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
