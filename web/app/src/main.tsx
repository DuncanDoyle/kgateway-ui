import { defaultPages, Shell } from "@kgateway-ui/shell";
import { createRoot } from "react-dom/client";
import { kgatewayBrand } from "./brand";

const extension = { brand: kgatewayBrand };

createRoot(document.getElementById("root") as HTMLElement).render(<Shell extension={extension} pages={defaultPages} />);
