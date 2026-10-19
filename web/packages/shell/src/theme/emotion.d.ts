import "@emotion/react";
import type { Theme as KgTheme } from "./tokens";

declare module "@emotion/react" {
  export interface Theme extends KgTheme {}
}
