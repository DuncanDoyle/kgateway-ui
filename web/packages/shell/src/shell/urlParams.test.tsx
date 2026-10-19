import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Link, MemoryRouter, useLocation } from "react-router-dom";
import { useUrlParams, useUrlText } from "./urlParams";

function Where() {
  return <output data-testid="location">{useLocation().search}</output>;
}

function Probe() {
  const [params, update] = useUrlParams();
  const [text, setText] = useUrlText(params, update, "q");
  return (
    <>
      {/* Two writes before the router commits: the second must keep the first. */}
      <button
        type="button"
        onClick={() => {
          update({ kind: "HTTPRoute" });
          update({ sev: "error" });
        }}
      >
        both
      </button>
      <button type="button" onClick={() => update({ kind: null })}>
        clear kind
      </button>
      <input aria-label="q" value={text} onChange={(e) => setText(e.target.value)} />
      <Link to="?sev=info">outside</Link>
      <output data-testid="kind">{params.get("kind") ?? ""}</output>
      <Where />
    </>
  );
}

function mount(path = "/") {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Probe />
    </MemoryRouter>,
  );
}

it("keeps every update made before the router commits", async () => {
  mount();
  await userEvent.click(screen.getByRole("button", { name: "both" }));
  expect(screen.getByTestId("location")).toHaveTextContent("?kind=HTTPRoute&sev=error");
  await userEvent.type(screen.getByRole("textbox", { name: "q" }), "cart");
  expect(screen.getByTestId("location")).toHaveTextContent("?kind=HTTPRoute&sev=error&q=cart");
  expect(screen.getByRole("textbox", { name: "q" })).toHaveValue("cart");
  await userEvent.click(screen.getByRole("button", { name: "clear kind" }));
  expect(screen.getByTestId("location")).toHaveTextContent("?sev=error&q=cart");
});

it("an outside navigation replaces the params and the text", async () => {
  mount("/?q=cart&kind=Gateway");
  expect(screen.getByRole("textbox", { name: "q" })).toHaveValue("cart");
  await userEvent.click(screen.getByRole("link", { name: "outside" }));
  expect(screen.getByTestId("location")).toHaveTextContent("?sev=info");
  expect(screen.getByRole("textbox", { name: "q" })).toHaveValue("");
  expect(screen.getByTestId("kind")).toBeEmptyDOMElement();
});
