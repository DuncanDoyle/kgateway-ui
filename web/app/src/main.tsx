import { defaultPages, kgatewayWebPack, renderWhenFontsReady, Shell, type ShellExtension } from "@snellius/shell";
import { snelliusBrand } from "./brand";

// One bundle for every install: the kgateway pack's copy and tabs show where an install has the features.
const extension: ShellExtension = { ...kgatewayWebPack, brand: snelliusBrand };

// The shell waits for the bundled fonts: a later swap reflows the page, e.g. wraps a long name in a detail header.
void renderWhenFontsReady(
  document.getElementById("root") as HTMLElement,
  <Shell extension={extension} pages={defaultPages} />,
);
