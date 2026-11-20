// A connected service or account as a compact row (GlimStone's ConnectionRow,
// "Connections"): its mark lit on a tile the row's whole height, its name with
// any badges, one quiet line on how it stands, and Test, Edit and Remove at the
// end. A button stands only where its handler is passed, and the row's own
// buttons, such as signing in again, stand before them and report on
// themselves as the test does.
import { useEffect, useState, type CSSProperties, type ReactNode } from 'react';
import type { TileMark } from '../lib/catalogs/catalog';
import { IconCheck, IconClose, IconPencil, IconTrash } from '../lib/icons';
import { usePhoneLayout } from '../lib/phoneLayout';
import { TileMarkView } from './CatalogTiles';
import type { Paint } from './HosterIcon';
import { Button } from './ui';

/** Where the row's test stands, as on a window's test action. */
export interface ConnectionTest {
  /** Set while it runs. The words, and so the width, stay. */
  busy?: boolean;
  /** How the last run came out. */
  tone?: 'ok' | 'danger';
  /** What the last run found, in the button's bubble. */
  title?: string;
}

/** A button of the row's own, which reports on itself as the test does. */
export interface ConnectionAction extends ConnectionTest {
  /** Tells the buttons apart and names the button's `data-connection` hook. */
  id: string;
  label: string;
  icon: ReactNode;
  disabled?: boolean;
  onClick: () => void;
}

export interface ConnectionRowText {
  test: string;
  edit: string;
  remove: string;
}

const isTileMark = (mark: unknown): mark is TileMark =>
  typeof mark === 'object' && mark !== null && typeof (mark as TileMark).svg === 'string';

export function ConnectionRow({
  name,
  mark,
  paint,
  status,
  badges,
  children,
  test,
  text,
  actions,
  onTest,
  onEdit,
  onRemove,
}: {
  /** The service's name, also the row's accessible name. */
  name: string;
  /** A brand's mark from a catalog, which stands on its own colour, or a logo or glyph of the app's, which stands on `paint` or the accent. */
  mark: TileMark | ReactNode;
  /** The colours a mark that is not a catalog's lights its tile in, such as a site's logo. */
  paint?: Paint;
  /** One quiet line under the name, such as who is signed in. */
  status?: string;
  /** Badges after the name, for a state that is not the normal case. */
  badges?: ReactNode;
  /** More quiet lines under the status. */
  children?: ReactNode;
  test?: ConnectionTest;
  text: ConnectionRowText;
  actions?: ConnectionAction[];
  onTest?: () => void;
  onEdit?: () => void;
  onRemove?: () => void;
}) {
  // On a phone the buttons show their glyphs alone, so they keep to one line.
  const compact = usePhoneLayout();
  const colours = isTileMark(mark) ? (mark.tile && mark.ink ? { tile: mark.tile, ink: mark.ink } : undefined) : paint;
  const tile = colours && ({ '--tile': colours.tile, '--tile-ink': colours.ink } as CSSProperties);
  const buttons: ConnectionAction[] = [
    ...(actions ?? []),
    ...(onTest ? [{ ...test, id: 'test', label: text.test, icon: <IconCheck />, onClick: onTest }] : []),
    ...(onEdit ? [{ id: 'edit', label: text.edit, icon: <IconPencil />, onClick: onEdit }] : []),
    ...(onRemove ? [{ id: 'remove', label: text.remove, icon: <IconTrash />, onClick: onRemove }] : []),
  ];
  const reporting = onTest || actions?.some((a) => a.busy !== undefined || a.tone !== undefined);
  const found = buttons
    .filter((b) => !b.busy && b.tone)
    .map((b) => b.title)
    .join(' ');

  return (
    <div data-glim="ConnectionRow" role="group" aria-label={name} className="glim-well glim-connection">
      <span className="glim-connection-tile" style={tile} aria-hidden>
        <span className={`glim-connection-mark ${isTileMark(mark) && mark.wordmark ? 'glim-connection-mark--wordmark' : ''}`}>
          {isTileMark(mark) ? <TileMarkView mark={mark} /> : mark}
        </span>
      </span>
      <div className="glim-connection-main">
        <div className="glim-connection-head">
          <span className="glim-connection-name">{name}</span>
          {badges && <span className="glim-connection-badges">{badges}</span>}
        </div>
        <div className="glim-connection-foot">
          <div className="glim-connection-about">
            {status && <p className="glim-connection-status">{status}</p>}
            {children}
          </div>
          {buttons.length > 0 && (
            <span className="glim-connection-actions">
              {buttons.map((b) => (
                <RowButton key={b.id} button={b} compact={compact} />
              ))}
            </span>
          )}
          {reporting && (
            <span role="status" className="sr-only">
              {found}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

/**
 * RowButton is one button at the row's end. While it runs it holds, then it
 * turns green with a check or red with a cross, and every failure that comes
 * back shakes it once more.
 */
function RowButton({ button: b, compact }: { button: ConnectionAction; compact: boolean }) {
  const tone = b.busy ? undefined : b.tone;
  const [shake, setShake] = useState(0);
  useEffect(() => {
    if (tone === 'danger') setShake((n) => n + 1);
  }, [tone, b.title]);
  return (
    <Button
      kind="secondary"
      icon={tone === 'danger' ? <IconClose /> : tone === 'ok' ? <IconCheck /> : b.icon}
      verdict={tone === 'ok' ? 'ok' : tone === 'danger' ? 'fail' : undefined}
      disabled={b.busy || b.disabled}
      shake={shake}
      data-connection={b.id}
      onClick={b.onClick}
      // Words beside the glyph, or the glyph alone with its words as its name.
      {...(compact ? { title: b.label } : { children: b.label, title: b.title })}
    />
  );
}
