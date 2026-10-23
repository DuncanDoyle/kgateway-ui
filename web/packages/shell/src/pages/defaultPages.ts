import type { CorePages } from "../shell/extension";
import { ClusterLanding } from "./ClusterLanding";
import { GatewayPage } from "./explore/GatewayPage";
import { GatewaysPage } from "./explore/GatewaysPage";
import { ListenerSetPage } from "./explore/ListenerSetPage";
import { ListenerSetsPage } from "./explore/ListenerSetsPage";
import { RoutePage } from "./explore/RoutePage";
import { RoutesPage } from "./explore/RoutesPage";
import { FindingsPage } from "./FindingsPage";
import { InstallPage } from "./install/InstallPage";
import { ResourcePage } from "./ResourcePage";

/** The upstream page implementations. */
export const defaultPages: Required<CorePages> = {
  ClusterLanding,
  InstallPage,
  FindingsPage,
  ResourcePage,
  GatewaysPage,
  GatewayPage,
  ListenerSetsPage,
  ListenerSetPage,
  RoutesPage,
  RoutePage,
};
