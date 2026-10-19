import { css, useTheme } from "@emotion/react";
import styled from "@emotion/styled";
import { Link, NavLink, useMatch } from "react-router-dom";
import { useNow } from "../api/clock";
import { isStale, useInstalls } from "../api/hooks";
import type { ReadyCount } from "../api/types";
import { HealthIcon } from "../health/HealthIcon";
import { ResourceName } from "../resource/ResourceName";
import { Shimmer } from "../states/Shimmer";
import { useShellExtension } from "./extension";
import { HEADER_HOVER_PCT, HEADER_PRESS_PCT, headerResourceName, headerTint } from "./headerTint";
import { InstallSwitcher } from "./InstallSwitcher";
import { ThemeSwitcher } from "./ThemeSwitcher";

// region Styles
const Bar = styled.header`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(3)};
  height: 52px;
  padding: 0 ${(p) => p.theme.space(4)};
  background: ${(p) => p.theme.color.headerBg};
  color: ${(p) => p.theme.color.headerText};
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  font-size: ${(p) => p.theme.font.size.md};
`;
const Brand = styled(Link)`
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 8px;
  margin-left: -8px;
  border-radius: ${(p) => p.theme.radius.sm};
  color: inherit;
  text-decoration: none;
  font-size: ${(p) => p.theme.font.size.base};
  font-weight: ${(p) => p.theme.font.weight.bold};
  letter-spacing: -0.01em;
  img, svg { height: 24px; width: auto; }
  &:hover { background: ${(p) => headerTint(p.theme, HEADER_HOVER_PCT)}; }
  &:active { background: ${(p) => headerTint(p.theme, HEADER_PRESS_PCT)}; }
`;
const Identity = styled.div`
  display: flex;
  align-items: center;
  gap: ${(p) => p.theme.space(2)};
  min-width: 0;
  padding-left: ${(p) => p.theme.space(3)};
  border-left: 1px solid ${(p) => p.theme.color.borderStrong};
  white-space: nowrap;
`;
const Chip = styled.span`
  padding: 2px 8px;
  border-radius: ${(p) => p.theme.radius.sm};
  font-size: ${(p) => p.theme.font.size.sm};
  font-weight: ${(p) => p.theme.font.weight.semibold};
  background: ${(p) => p.theme.color.accent};
  color: ${(p) => p.theme.color.headerText};
`;
const Tag = styled.span`
  padding: 1px 7px;
  border-radius: ${(p) => p.theme.radius.sm};
  font-size: ${(p) => p.theme.font.size.sm};
  border: 1px solid ${(p) => p.theme.color.borderStrong};
  color: ${(p) => p.theme.color.headerText};
  @media (max-width: 860px) { display: none; }
`;
const Ready = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: ${(p) => p.theme.font.size.sm};
  color: ${(p) => p.theme.color.headerText};
`;
const Spacer = styled.div`
  flex: 1;
`;
const Nav = styled.nav`
  display: flex;
  gap: ${(p) => p.theme.space(1)};
  min-width: 0;
  a {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    border-radius: ${(p) => p.theme.radius.sm};
    color: ${(p) => p.theme.color.headerText};
    text-decoration: none;
  }
  a:hover { background: ${(p) => headerTint(p.theme, HEADER_HOVER_PCT)}; }
  a:active { background: ${(p) => headerTint(p.theme, HEADER_PRESS_PCT)}; }
  a[aria-current="page"] { box-shadow: inset 0 -2px 0 ${(p) => p.theme.color.headerText}; }
`;

// region Helpers
/** Matches KGWUI-005: fewer ready replicas than desired is an error. */
function readiness(c: ReadyCount): "healthy" | "error" {
  return c.ready >= c.desired ? "healthy" : "error";
}

/** The version label may or may not carry a leading "v"; show exactly one. */
function versionText(v: string): string {
  return `v${v.replace(/^v/, "")}`;
}

// region Component
/** Header: brand, install identity, version, edition and controller readiness, extension navigation, theme switcher. */
export function Header() {
  const { brand, navEntries } = useShellExtension();
  const match = useMatch("/i/:ns/:name/*");
  const { data } = useInstalls();
  const now = useNow();
  const id = match ? `${match.params.ns}/${match.params.name}` : "";
  const current = data?.find((i) => i.id === id);
  const theme = useTheme();
  const nav = navEntries.filter((n) => n.scope === "top" || id);
  return (
    <Bar>
      <Brand to="/">
        {brand.logo}
        <span>{brand.productName}</span>
      </Brand>
      {id && !data ? (
        <Identity>
          <Shimmer
            h={20}
            stylesOverride={css`
              width: 320px;
              background: ${headerTint(theme, HEADER_HOVER_PCT)};
            `}
          />
        </Identity>
      ) : null}
      {current ? (
        <Identity>
          {data && data.length > 1 ? (
            <InstallSwitcher installs={data} currentId={id} />
          ) : (
            <ResourceName
              namespace={current.namespace}
              name={current.name}
              stylesOverride={headerResourceName(theme)}
            />
          )}
          {current.version ? <Chip data-testid="version">{versionText(current.version)}</Chip> : null}
          <Tag data-testid="edition">{current.edition}</Tag>
          <Ready>
            <HealthIcon state={isStale(current.computedAt, now) ? "unknown" : readiness(current.controller)} />
            Controller {current.controller.ready}/{current.controller.desired} ready
          </Ready>
        </Identity>
      ) : null}
      {nav.length > 0 ? (
        <Nav aria-label="Extensions">
          {nav.map((n) => (
            <NavLink key={`${n.scope}:${n.to}`} to={n.scope === "install" ? `/i/${id}/${n.to}` : `/${n.to}`}>
              {n.icon}
              {n.label}
            </NavLink>
          ))}
        </Nav>
      ) : null}
      <Spacer />
      <ThemeSwitcher />
    </Bar>
  );
}
