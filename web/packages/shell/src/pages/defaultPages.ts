import type { CorePages } from "../shell/extension";
import { ClusterLanding } from "./ClusterLanding";
import { FindingsPage } from "./FindingsPage";
import { InstallPage } from "./install/InstallPage";
import { ResourcePage } from "./ResourcePage";

/** The upstream page implementations. */
export const defaultPages: CorePages = { ClusterLanding, InstallPage, FindingsPage, ResourcePage };
