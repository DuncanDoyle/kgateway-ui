import type { Brand } from "@kgateway-ui/shell";
import logoUrl from "./assets/kgateway-icon-white.svg";

/** The kgateway project identity: the CNCF artwork icon (white variant for the ink header), identifying the project this UI is for. */
export const kgatewayBrand: Brand = {
  productName: "kgateway",
  logo: <img src={logoUrl} alt="" width={24} height={24} />,
};
