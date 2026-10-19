/// <reference types="vite/client" />
import html from "../../../../app/index.html?raw";
import { controlRoomDark, controlRoomLight } from "./themes";

// Colours the pre-React script paints must equal the themes' bg tokens, or the page flashes.
it("pre-React background colours equal the theme bg tokens", () => {
  expect(html).toContain(`"${controlRoomDark.color.bg}"`);
  expect(html).toContain(`"${controlRoomLight.color.bg}"`);
});

it("pre-React script knows the same theme ids and storage key as the provider", () => {
  expect(html).toContain(`"${controlRoomDark.id}"`);
  expect(html).toContain(`"${controlRoomLight.id}"`);
  expect(html).toContain('"kgateway-ui.theme"');
  expect(html).toContain("colorScheme");
});
