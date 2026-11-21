import type { MouseEvent, ReactNode } from 'react';
import { Button, Card, InfoBubble, SectionTitle, ToggleRow } from '../../../components/ui';
import { openExternal } from '../../../lib/external';
import { IconBook, IconInstall, IconRetry, IconSync, IconUpdate, IconWhatsNew } from '../../../lib/icons';
import {
  autoSwitch,
  checkButton,
  componentRows,
  designRow,
  numberLink,
  updateAction,
  updateLine,
  whatsNewLabel,
  withApp,
  type ComponentRow,
  type ComponentsCardText,
  type UpdateRoute,
  type UpdateState,
  type VersionCardText,
} from '../../../lib/versionCards';

// GlimStone's InfoVersionCard and InfoComponentsCard (packages/react/src/
// AboutCard.tsx), drawn with this UI's own Card, Button and ToggleRow. The
// classes are the ones tokens.css gives them, copied into index.css.

/**
 * VersionNumber reads as plain text and opens the newest release of its
 * project. It takes the accent's ink under the pointer, never an underline.
 */
export function VersionNumber({ row, unreleased }: { row: Pick<ComponentRow, 'version' | 'repoUrl' | 'releasePage'>; unreleased: (version: string) => string }) {
  const { shown, released, href } = numberLink(row);
  if (!shown) return null;
  if (!href) {
    return (
      <span dir="ltr" className="glim-num glim-version-number">
        {released ? shown : unreleased(shown)}
      </span>
    );
  }
  return (
    <a
      dir="ltr"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      onClick={(e: MouseEvent<HTMLAnchorElement>) => {
        e.preventDefault();
        openExternal(href);
      }}
      className="glim-num glim-version-number"
    >
      {shown}
    </a>
  );
}

const ACTION_GLYPH = { update: <IconInstall />, restart: <IconRetry />, guide: <IconBook /> };

/** The dot before "What's new": green while something is unseen, grey once nothing is. */
function WhatsNewGlyph({ unseen }: { unseen: boolean }) {
  return (
    <>
      <span aria-hidden className="glim-version-dot me-1.5" data-unseen={unseen ? '' : undefined} />
      <IconWhatsNew />
    </>
  );
}

export interface InfoVersionCardProps {
  text: VersionCardText;
  name: string;
  version: string;
  repoUrl?: string;
  route: UpdateRoute;
  state: UpdateState;
  onCheck?: () => void;
  onUpdate?: () => void;
  onRestart?: () => void;
  guideUrl?: string;
  onWhatsNew: () => void;
  unseen?: number;
  autoUpdate?: boolean;
  onAutoUpdateChange?: (on: boolean) => void;
  hue?: number;
  'data-new'?: string;
}

/**
 * InfoVersionCard is the Info tile's first card: the app's name and version,
 * the switch, and a last row with where the updates stand at its start and the
 * buttons at its end: "What's new", "Check for updates" while there is
 * something to look for, and the one action once there is something to do.
 */
export function InfoVersionCard({
  text,
  name,
  version,
  repoUrl,
  route,
  state,
  onCheck,
  onUpdate,
  onRestart,
  guideUrl,
  onWhatsNew,
  unseen = 0,
  autoUpdate,
  onAutoUpdateChange,
  hue,
  'data-new': mark,
}: InfoVersionCardProps) {
  const face = updateLine(state, text);
  const action = updateAction(route, state, text);
  const toggle = autoSwitch(route, text);
  const check = checkButton(route, state);
  const press = () => {
    if (action?.kind === 'guide') {
      if (guideUrl) openExternal(guideUrl);
    } else if (action?.kind === 'restart') onRestart?.();
    else onUpdate?.();
  };
  return (
    <Card hue={hue} data-new={mark} className="flex flex-col gap-4">
      <SectionTitle hint={withApp(text.hint, name)}>{text.title}</SectionTitle>
      <div className="glim-version-head">
        <span className="glim-version-app">{name}</span>
        <VersionNumber row={{ version, repoUrl }} unreleased={text.unreleased} />
      </div>
      {toggle && autoUpdate !== undefined && (
        <div data-new="update-switch">
          <ToggleRow
            label={toggle.label}
            hint={withApp(toggle.hint, name)}
            checked={autoUpdate}
            onChange={(on) => onAutoUpdateChange?.(on)}
          />
        </div>
      )}
      {state.kind === 'downloading' && (
        <div
          className="glim-version-progress"
          role="progressbar"
          aria-label={face.line}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={state.percent}
        >
          <span style={{ width: `${state.percent}%` }} />
        </div>
      )}
      <div className="glim-version-foot">
        <p className="glim-version-state" data-tone={face.tone} role="status">
          {face.line && <span className="glim-version-line">{face.line}</span>}
          {face.note && <span className="glim-version-note">{face.note}</span>}
        </p>
        <div className="glim-version-actions">
          <Button
            kind="secondary"
            data-new="news-button"
            icon={<WhatsNewGlyph unseen={unseen > 0} />}
            label={whatsNewLabel(text, unseen)}
            onClick={onWhatsNew}
          >
            {whatsNewLabel(text, unseen)}
          </Button>
          {check && onCheck && (
            <Button kind="secondary" icon={<IconSync />} disabled={check.busy} aria-busy={check.busy || undefined} onClick={onCheck}>
              {text.check}
            </Button>
          )}
          {action && (
            <Button kind={action.kind === 'guide' ? 'secondary' : 'primary'} icon={ACTION_GLYPH[action.kind]} onClick={press}>
              {action.label}
            </Button>
          )}
        </div>
      </div>
    </Card>
  );
}

/** A Components row with what this UI adds to it: its own controls, such as qBittorrent's version picker. */
export type ComponentsCardRow = ComponentRow & { actions?: ReactNode; 'data-new'?: string };

export interface InfoComponentsCardProps {
  text: ComponentsCardText;
  name: string;
  rows: ComponentsCardRow[];
  autoUpdate?: boolean;
  onAutoUpdateChange?: (on: boolean) => void;
  onUpdate?: () => void;
  updating?: boolean;
  /** Each bump shakes "Update components" once, after an update that failed somewhere. */
  shake?: number;
  /** Stands under the rows where they wait for something, such as how this instance runs. */
  children?: ReactNode;
  hue?: number;
  'data-new'?: string;
}

/**
 * InfoComponentsCard is the second card: every version the app is made of or
 * brings along, as plain numbers in a fixed order, with a status and what to
 * do on a row that needs it.
 */
export function InfoComponentsCard({
  text,
  name,
  rows,
  autoUpdate,
  onAutoUpdateChange,
  onUpdate,
  updating = false,
  shake,
  children,
  hue,
  'data-new': mark,
}: InfoComponentsCardProps) {
  return (
    <Card hue={hue} data-new={mark} className="flex flex-col gap-4">
      <SectionTitle hint={withApp(text.hint, name)}>{text.title}</SectionTitle>
      {children}
      <ul data-new="built-in" className="glim-version-rows">
        {componentRows<ComponentsCardRow>([...rows, designRow(text)]).map((row) => (
          <li key={row.name} data-new={row['data-new']} className="glim-version-row">
            <span className="glim-version-name">
              <span className="glim-version-part">{row.name}</span>
              {row.hint && <InfoBubble tip={row.hint} />}
            </span>
            {row.status && (
              <span className="glim-version-status" data-tone={row.tone} role="status">
                {row.status}
                {row.detail && <InfoBubble tip={row.detail} />}
              </span>
            )}
            {row.actions && <span className="glim-version-row-actions">{row.actions}</span>}
            <VersionNumber row={row} unreleased={text.unreleased} />
          </li>
        ))}
      </ul>
      {autoUpdate !== undefined && (
        <div data-new="components-switch">
          <ToggleRow label={text.auto} hint={withApp(text.autoHint, name)} checked={autoUpdate} onChange={(on) => onAutoUpdateChange?.(on)} />
        </div>
      )}
      {onUpdate && (
        <div className="glim-version-actions">
          <Button kind="secondary" icon={<IconUpdate />} disabled={updating} aria-busy={updating || undefined} shake={shake} onClick={onUpdate}>
            {text.update}
          </Button>
        </div>
      )}
    </Card>
  );
}
