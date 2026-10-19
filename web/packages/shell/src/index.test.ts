import * as shell from "./index";

it("exports the shell API", () => {
  expect(shell.SHELL_API_VERSION).toBe(1);
});
