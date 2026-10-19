import type { SerializedStyles } from "@emotion/react";
import styled from "@emotion/styled";

// region Styles
const Name = styled.span<{ stylesOverride?: SerializedStyles }>`
  font-family: ${(p) => p.theme.font.mono};
  font-size: ${(p) => p.theme.font.size.code};
  white-space: nowrap;
  [data-part="namespace"] { opacity: 0.55; }
  ${(p) => p.stylesOverride}
`;

// region Component
interface ResourceNameProps {
  namespace?: string;
  name: string;
  stylesOverride?: SerializedStyles;
}

/** The signature element: mono resource name with the namespace dimmed (D-011). */
export function ResourceName({ namespace, name, stylesOverride }: ResourceNameProps) {
  return (
    // The label keeps the accessible name "namespace/name" whole, whatever the inner markup.
    <Name aria-label={namespace ? `${namespace}/${name}` : name} stylesOverride={stylesOverride}>
      {namespace ? <span data-part="namespace">{namespace}/</span> : null}
      <span data-part="name">{name}</span>
    </Name>
  );
}
