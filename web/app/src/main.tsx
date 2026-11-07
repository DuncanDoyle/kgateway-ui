import { defaultPages, renderWhenFontsReady, Shell } from "@snellius/shell";
import { snelliusBrand } from "./brand";

const extension = { brand: snelliusBrand };

// The shell waits for the bundled fonts: a later swap reflows the page, e.g. wraps a long name in a detail header.
void renderWhenFontsReady(
  document.getElementById("root") as HTMLElement,
  <Shell extension={extension} pages={defaultPages} />,
);
