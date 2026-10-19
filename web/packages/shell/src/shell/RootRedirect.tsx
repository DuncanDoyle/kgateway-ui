import { Navigate } from "react-router-dom";
import { useInstalls, useMeta } from "../api/hooks";
import { PageState } from "../states/PageState";
import type { CorePages } from "./extension";

// region Component
/** "/" → the single (or pinned) install, else the cluster landing (design §6.4). */
export function RootRedirect({ ClusterLanding }: Pick<CorePages, "ClusterLanding">) {
  const { data, error } = useInstalls();
  const { data: meta } = useMeta();
  // Loaded installs stay in use after a failed poll; the landing shows the error over them.
  if (!data) {
    if (error?.status === 503)
      return <PageState kind="connecting" title="Waiting for the cluster" message={error.message} />;
    if (error)
      return <PageState kind="error" title="Cannot load installs" message={error.message} copyText={error.message} />;
    return <PageState kind="loading" title="Loading installs…" />;
  }
  if (data.length === 0) {
    return (
      <PageState
        kind="empty"
        title="No kgateway installation found"
        message={
          <>
            The UI looks for controller Deployments labelled <code>kgateway=kgateway</code> and{" "}
            <code>app.kubernetes.io/component=controller</code>.
            {meta?.pinned ? (
              <>
                {" "}
                It is pinned to <code>{meta.pinned}</code> with <code>--install</code>, which was not found.
              </>
            ) : (
              <>
                {" "}
                To show one specific install, start the UI with <code>--install namespace/name</code>.
              </>
            )}
          </>
        }
      />
    );
  }
  if (data.length === 1) return <Navigate to={`/i/${data[0]?.id}`} replace />;
  return <ClusterLanding />;
}
