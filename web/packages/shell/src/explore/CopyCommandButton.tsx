import type { SerializedStyles } from "@emotion/react";
import { Terminal } from "lucide-react";
import { useMeta } from "../api/hooks";
import type { Ref } from "../api/types";
import { IconButton } from "../resource/CopyNameButton";
import { useVocabulary } from "../shell/extension";
import { Tooltip } from "../shell/Tooltip";

// region Component
interface CopyCommandButtonProps {
  refValue: Ref;
  placement?: "center" | "end";
  stylesOverride?: SerializedStyles;
}

/** Icon button that copies the platform's command for refValue; hidden when /meta does not serve the kind or getCommand returns undefined. */
export function CopyCommandButton({ refValue, placement = "center", stylesOverride }: CopyCommandButtonProps) {
  const { getCommand, copyCommand: label } = useVocabulary();
  const { data: meta } = useMeta();
  const served = meta?.kinds.find((k) => k.group === refValue.group && k.kind === refValue.kind);
  const command = served ? getCommand(refValue, served.resource) : undefined;
  if (command === undefined) return null;
  return (
    <Tooltip label={label} placement={placement}>
      <IconButton
        type="button"
        aria-label={label}
        stylesOverride={stylesOverride}
        onClick={() => void navigator.clipboard?.writeText(command).catch(() => {})}
      >
        <Terminal size={14} aria-hidden="true" />
      </IconButton>
    </Tooltip>
  );
}
