import type { Brand } from "@snellius/shell";
import logoUrl from "./assets/snellius/snellius-symbol-dark.svg";

/** The Snellius identity: the prism symbol (dark variant, white outline, for the ink header) and the lowercase wordmark. */
export const snelliusBrand: Brand = {
  productName: "snellius",
  logo: <img src={logoUrl} alt="" width={24} height={24} />,
};
