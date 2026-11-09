import { defaultPages, renderWhenFontsReady, Shell } from "@snellius/shell";
import { appExtension } from "./extension";

// The shell waits for the bundled fonts: a later swap reflows the page, e.g. wraps a long name in a detail header.
void renderWhenFontsReady(
  document.getElementById("root") as HTMLElement,
  <Shell extension={appExtension} pages={defaultPages} />,
);
