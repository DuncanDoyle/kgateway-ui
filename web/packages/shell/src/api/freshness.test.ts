import { confirmedOrComputed } from "./freshness";

test("confirmedOrComputed prefers confirmedAt", () => {
  expect(confirmedOrComputed({ computedAt: "a", confirmedAt: "b" })).toBe("b");
});

test("confirmedOrComputed falls back to computedAt when confirmedAt is absent", () => {
  expect(confirmedOrComputed({ computedAt: "a" })).toBe("a");
});
