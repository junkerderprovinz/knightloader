// A connected service or account as a compact row (GlimStone's ConnectionRow,
// "Connections"): its mark lit on a tile the row's whole height, its name with
// any badges, one quiet line on how it stands, and Test, Edit and Remove at the
// end. A button stands only where its handler is passed.
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
  onTest?: () => void;
  onEdit?: () => void;
  onRemove?: () => void;
}) {
  // On a phone the buttons show their glyphs alone, so they keep to one line.
  const compact = usePhoneLayout();
  const colours = isTileMark(mark) ? (mark.tile && mark.ink ? { tile: mark.tile, ink: mark.ink } : undefined) : paint;
  const tile = colours && ({ '--tile': colours.tile, '--tile-ink': colours.ink } as CSSProperties);
  const tone = test?.busy ? undefined : test?.tone;

  // Every failure that comes back shakes the button once more.
  const [shake, setShake] = useState(0);
  useEffect(() => {
    if (tone === 'danger') setShake((n) => n + 1);
  }, [tone, test]);

  // Words beside the glyph, or the glyph alone with its words as its name.
  const words = (label: string) => (compact ? { title: label } : { children: label });

  return (
    <div data-glim="ConnectionRow" role="group" aria-label={name} className="glim-well glim-connection">
      <span className="glim-connection-tile" style={tile} aria-hidden>
        <span className="glim-connection-mark">{isTileMark(mark) ? <TileMarkView mark={mark} /> : mark}</span>
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
          {(onTest || onEdit || onRemove) && (
            <span className="glim-connection-actions">
              {onTest && (
                <Button
                  kind="secondary"
                  icon={tone === 'danger' ? <IconClose /> : <IconCheck />}
                  verdict={tone === 'ok' ? 'ok' : tone === 'danger' ? 'fail' : undefined}
                  disabled={test?.busy}
                  shake={shake}
                  data-connection="test"
                  onClick={onTest}
                  {...words(text.test)}
                  title={compact ? text.test : test?.title}
                />
              )}
              {onEdit && <Button kind="secondary" icon={<IconPencil />} data-connection="edit" onClick={onEdit} {...words(text.edit)} />}
              {onRemove && (
                <Button kind="secondary" icon={<IconTrash />} data-connection="remove" onClick={onRemove} {...words(text.remove)} />
              )}
            </span>
          )}
          {onTest && (
            <span role="status" className="sr-only">
              {tone ? test?.title : ''}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}
