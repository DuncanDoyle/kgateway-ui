import { apiFetch, PageState, POLL_MS, ResourceName, type ShellExtension } from "@snellius/shell";
import useSWR from "swr";

/** The body of GET /api/ext/acme/widgets, served by acme.go. */
interface WidgetList {
  widgets: { namespace: string; name: string }[];
}

/** Lists the widgets in the Gateway's namespace; the backend already hid what the viewer may not see. */
function WidgetsTab({ namespace }: { namespace?: string }) {
  const { data, error } = useSWR<WidgetList>("/api/ext/acme/widgets", apiFetch, { refreshInterval: POLL_MS });
  if (error) return <PageState kind="error" title="Cannot load widgets" message={String(error.message)} />;
  if (!data) return <PageState kind="loading" title="Loading widgets" />;
  const here = data.widgets.filter((w) => w.namespace === namespace);
  if (here.length === 0) return <PageState kind="empty" title="No widgets in this namespace" />;
  return (
    <ul aria-label="Widgets">
      {here.map((w) => (
        <li key={w.name}>
          <ResourceName namespace={w.namespace} name={w.name} />
        </li>
      ))}
    </ul>
  );
}

/** What this build adds to the stock UI. */
export const acme: ShellExtension = {
  brand: { productName: "acme", logo: null },
  contributions: {
    // A tab on every Gateway page, after the core tabs
    detailTabs: {
      "gateway.networking.k8s.io/Gateway": [
        { id: "widgets", label: "Widgets", render: ({ ref }) => <WidgetsTab namespace={ref.namespace} /> },
      ],
    },
  },
};
