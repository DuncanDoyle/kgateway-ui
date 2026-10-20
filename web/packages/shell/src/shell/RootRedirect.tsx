import { Navigate } from "react-router-dom";
import { useInstalls, useMeta } from "../api/hooks";
import { PageState } from "../states/PageState";
import { type CorePages, useVocabulary } from "./extension";
import { installPath } from "./paths";

// region Component
/** "/" → the single (or pinned) install, else the cluster landing (design §6.4). */
export function RootRedirect({ ClusterLanding }: Pick<CorePages, "ClusterLanding">) {
  const { data, error } = useInstalls();
  const { data: meta } = useMeta();
  const vocab = useVocabulary();
  // Loaded installs stay in use after a failed poll; the landing shows the error over them.
  if (!data) {
    if (error?.status === 503) return <PageState kind="connecting" title={vocab.connecting} message={error.message} />;
    if (error)
      return <PageState kind="error" title="Cannot load installs" message={error.message} copyText={error.message} />;
    return <PageState kind="loading" title="Loading installs…" />;
  }
  if (data.length === 0) {
    return (
      <PageState
        kind="empty"
        title={vocab.noInstallsTitle}
        message={
          <>
            {vocab.noInstallsHelp} {vocab.pinHint(meta?.pinned)}
          </>
        }
      />
    );
  }
  if (data.length === 1 && data[0]) return <Navigate to={installPath(data[0].id)} replace />;
  return <ClusterLanding />;
}
