import { kgatewayWebPack, type ShellExtension } from "@snellius/shell";
import { snelliusBrand } from "./brand";

/** The default app's extension: the Snellius brand and the kgateway pack, which /meta switches on per server. */
export const appExtension: ShellExtension = { brand: snelliusBrand, packs: [kgatewayWebPack] };
