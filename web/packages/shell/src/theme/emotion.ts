import type { Theme as KgTheme } from "./tokens";

// Types emotion's theme as the shell Theme; a .ts file so the build ships it in dist.
declare module "@emotion/react" {
  export interface Theme extends KgTheme {}
}
