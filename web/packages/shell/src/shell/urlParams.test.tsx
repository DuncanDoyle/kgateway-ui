import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Suspense, useState } from "react";
import { Link, MemoryRouter, useLocation, useSearchParams } from "react-router-dom";
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

// region Interrupted transitions

let release: () => void = () => {};
let gate: Promise<void> | undefined;

// Suspends while the committed URL asks for a "slow" search, so the router's transition render is discarded.
function Slow() {
  const [p] = useSearchParams();
  if (p.get("q") === "slow" && gate) throw gate;
  return null;
}

function Racer() {
  const [params, update] = useUrlParams();
  const [n, setN] = useState(0);
  return (
    <>
      <button type="button" onClick={() => update({ q: "slow" })}>
        slow
      </button>
      <button type="button" onClick={() => setN(n + 1)}>
        urgent {n}
      </button>
      <button type="button" onClick={() => update({ b: "2" })}>
        add b
      </button>
      <output data-testid="latest">{params.toString()}</output>
      <Suspense fallback={null}>
        <Slow />
      </Suspense>
      <Where />
    </>
  );
}

it("a discarded transition render loses no pending update", async () => {
  gate = new Promise<void>((r) => {
    release = r;
  });
  render(
    <MemoryRouter initialEntries={["/"]}>
      <Racer />
    </MemoryRouter>,
  );
  await userEvent.click(screen.getByRole("button", { name: "slow" }));
  // The transition is suspended: the committed URL is still empty.
  expect(screen.getByTestId("location")).toBeEmptyDOMElement();
  await userEvent.click(screen.getByRole("button", { name: /urgent/ }));
  // An urgent render in between still sees the pending write, never the stale committed URL.
  expect(screen.getByRole("button", { name: "urgent 1" })).toBeInTheDocument();
  expect(screen.getByTestId("latest")).toHaveTextContent("q=slow");
  await userEvent.click(screen.getByRole("button", { name: "add b" }));
  gate = undefined;
  await act(async () => release());
  await waitFor(() => expect(screen.getByTestId("location")).toHaveTextContent("?q=slow&b=2"));
});

function Counter() {
  const [, update] = useUrlParams();
  const loc = useLocation();
  return (
    <>
      <button type="button" onClick={() => update({ a: "1" })}>
        same
      </button>
      <output data-testid="key">{loc.key}</output>
    </>
  );
}

it("an update that changes nothing does not navigate", async () => {
  render(
    <MemoryRouter initialEntries={["/?a=1"]}>
      <Counter />
    </MemoryRouter>,
  );
  const before = screen.getByTestId("key").textContent;
  await userEvent.click(screen.getByRole("button", { name: "same" }));
  expect(screen.getByTestId("key").textContent).toBe(before);
});
