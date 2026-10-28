import { controlRoomDark, defaultPages, Shell } from "@kgateway-ui/shell";
import { createRoot } from "react-dom/client";
import { kgatewayBrand } from "./brand";

const extension = { brand: kgatewayBrand };
const { ui, mono } = controlRoomDark.font;

// Render once the bundled fonts are in: a later swap reflows the page, e.g. wraps a long name in a detail header.
Promise.all([ui, mono].map((f) => document.fonts.load(`1em ${f}`)))
  .catch(() => undefined)
  .then(() =>
    createRoot(document.getElementById("root") as HTMLElement).render(
      <Shell extension={extension} pages={defaultPages} />,
    ),
  );
