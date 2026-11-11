import type { CorePages } from "../shell/extension";
import { BackendPage } from "./backends/BackendPage";
import { BackendsPage } from "./backends/BackendsPage";
import { ClusterLanding } from "./ClusterLanding";
import { GatewayPage } from "./explore/GatewayPage";
import { GatewaysPage } from "./explore/GatewaysPage";
import { ListenerSetPage } from "./explore/ListenerSetPage";
import { ListenerSetsPage } from "./explore/ListenerSetsPage";
import { RoutePage } from "./explore/RoutePage";
import { RoutesPage } from "./explore/RoutesPage";
import { FindingPage } from "./findings/FindingPage";
import { FindingsPage } from "./findings/FindingsPage";
import { DiagnosticsSection, InstallPage } from "./install/InstallPage";
import { ProcessesPage } from "./processes/ProcessesPage";
import { ResourcePage } from "./ResourcePage";
import { TrafficPage } from "./traffic/TrafficPage";

/** The upstream page implementations. */
export const defaultPages: Required<CorePages> = {
  ClusterLanding,
  InstallPage,
  FindingsPage,
  FindingPage,
  ResourcePage,
  GatewaysPage,
  GatewayPage,
  ListenerSetsPage,
  ListenerSetPage,
  RoutesPage,
  RoutePage,
  DiagnosticsSection,
  ProcessesPage,
  TrafficPage,
  BackendsPage,
  BackendPage,
};
