import { css, type SerializedStyles, useTheme } from "@emotion/react";
import styled from "@emotion/styled";
import { ExternalLink } from "lucide-react";
import { useInstalls, useMeta } from "../api/hooks";
import type { InstallSummary } from "../api/types";
import { aboutCopy, aboutLinks } from "../copy/about";
import { useShellExtension } from "../shell/extension";
import { HEADER_MUTED_PCT, headerTint } from "../shell/headerTint";
import { InstallName } from "../shell/InstallName";
import { Shimmer } from "../states/Shimmer";

// region Styles
const Page = styled.div<{ stylesOverride?: SerializedStyles }>`
  max-width: 960px;
  margin: 0 auto;
  padding: ${(p) => p.theme.space(6)} ${(p) => p.theme.space(4)};
  display: grid;
  gap: ${(p) => p.theme.space(4)};
  grid-template-columns: repeat(auto-fit, minmax(min(360px, 100%), 1fr));
  ${(p) => p.stylesOverride}
`;
// The ink band in every theme, so a brand's header logo reads on it as it does in the header.
const Hero = styled.section`
  grid-column: 1 / -1;
  display: flex;
  flex-direction: column;
  gap: ${(p) => p.theme.space(2)};
  padding: ${(p) => p.theme.space(8)} ${(p) => p.theme.space(6)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.headerBg};
  color: ${(p) => p.theme.color.headerText};
  h1 {
    display: flex;
    align-items: center;
    gap: ${(p) => p.theme.space(4)};
    margin: 0;
    font-size: calc(2 * ${(p) => p.theme.font.size.xl});
    font-weight: ${(p) => p.theme.font.weight.bold};
    letter-spacing: -0.02em;
    line-height: 1.1;
    overflow-wrap: anywhere;
  }
  h1 img,
  h1 svg {
    flex: none;
    height: 64px;
    width: auto;
  }
  .descriptor {
    margin: ${(p) => p.theme.space(2)} 0 0;
    font-size: ${(p) => p.theme.font.size.lg};
    font-weight: ${(p) => p.theme.font.weight.semibold};
  }
  .tagline {
    margin: 0;
    font-size: ${(p) => p.theme.font.size.base};
    color: ${(p) => headerTint(p.theme, HEADER_MUTED_PCT)};
  }
`;
const Card = styled.section<{ $wide?: boolean }>`
  min-width: 0;
  padding: ${(p) => p.theme.space(4)};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.md};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => p.theme.color.text};
  ${(p) => (p.$wide ? "grid-column: 1 / -1;" : "")}
  h2 {
    margin: 0 0 ${(p) => p.theme.space(3)};
    font-size: ${(p) => p.theme.font.size.xs};
    font-weight: ${(p) => p.theme.font.weight.semibold};
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: ${(p) => p.theme.color.textMuted};
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
    display: grid;
    gap: ${(p) => p.theme.space(2)};
  }
  .value {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.base};
  }
  .muted {
    color: ${(p) => p.theme.color.textMuted};
    font-size: ${(p) => p.theme.font.size.sm};
  }
`;
const InstallRow = styled.li`
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: ${(p) => p.theme.space(1)} ${(p) => p.theme.space(3)};
  padding: ${(p) => p.theme.space(2)} 0;
  border-top: 1px solid ${(p) => p.theme.color.border};
  &:first-of-type { border-top: none; padding-top: 0; }
  .product { font-weight: ${(p) => p.theme.font.weight.semibold}; }
  .controller {
    font-family: ${(p) => p.theme.font.mono};
    font-size: ${(p) => p.theme.font.size.code};
  }
`;
const ProjectLink = styled.a`
  display: inline-flex;
  align-items: center;
  gap: ${(p) => p.theme.space(1)};
  color: ${(p) => p.theme.color.accentText};
  text-decoration: underline;
  text-underline-offset: 2px;
  border-radius: ${(p) => p.theme.radius.sm};
  &:hover {
    color: ${(p) => p.theme.color.text};
    text-decoration-thickness: 2px;
  }
  &:active { color: ${(p) => p.theme.color.textMuted}; }
  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.focus};
    outline-offset: 2px;
  }
`;
const nameStyle = (size: string) => css`
  font-size: ${size};
`;

// region Helpers
/** "kgateway 2.5.0": the edition and its version as the install reports them. */
function productText(i: InstallSummary): string {
  return [i.edition, i.version].filter(Boolean).join(" ");
}

// region Component
function Installs() {
  const { data, error } = useInstalls();
  const theme = useTheme();
  if (!data) return error ? <p className="muted">{aboutCopy.installsError}</p> : <Shimmer h={20} />;
  if (data.length === 0) return <p className="muted">{aboutCopy.noInstalls}</p>;
  return (
    <ul>
      {data.map((i) => (
        <InstallRow key={i.id}>
          <span className="product">{productText(i)}</span>
          <InstallName install={i} stylesOverride={nameStyle(theme.font.size.code)} />
          {i.controllerNames.length > 0 ? (
            <span className="muted">
              {aboutCopy.controller} <span className="controller">{i.controllerNames.join(", ")}</span>
            </span>
          ) : null}
        </InstallRow>
      ))}
    </ul>
  );
}

function LinkItem({ href, label }: { href: string; label: string }) {
  return (
    <li>
      <ProjectLink href={href} target="_blank" rel="noopener noreferrer">
        {label}
        <ExternalLink size={12} aria-hidden="true" />
      </ProjectLink>
    </li>
  );
}

interface AboutPageProps {
  stylesOverride?: SerializedStyles;
}

/** The About page (D-063): the brand, its descriptor and tagline when it sets them, the UI version, watched installs and project links. */
export function AboutPage({ stylesOverride }: AboutPageProps = {}) {
  const { brand } = useShellExtension();
  const { data: meta, error } = useMeta();
  return (
    <Page stylesOverride={stylesOverride}>
      <Hero>
        <h1>
          {brand.logo}
          <span>{brand.productName}</span>
        </h1>
        {brand.descriptor ? <p className="descriptor">{brand.descriptor}</p> : null}
        {brand.tagline ? <p className="tagline">{brand.tagline}</p> : null}
      </Hero>
      <Card>
        <h2>{aboutCopy.version}</h2>
        {meta ? (
          <span className="value">{meta.version}</span>
        ) : error ? (
          <span className="muted">{aboutCopy.versionUnknown}</span>
        ) : (
          <Shimmer h={20} />
        )}
      </Card>
      <Card>
        <h2>{aboutCopy.project}</h2>
        <ul>
          <LinkItem href={aboutLinks.source} label={aboutCopy.source} />
          <LinkItem href={aboutLinks.docs} label={aboutCopy.docs} />
          <LinkItem href={aboutLinks.licence} label={aboutCopy.licence} />
        </ul>
      </Card>
      <Card $wide>
        <h2>{aboutCopy.installs}</h2>
        <Installs />
      </Card>
    </Page>
  );
}
