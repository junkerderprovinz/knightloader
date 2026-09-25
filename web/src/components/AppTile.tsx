import type { ReactNode } from 'react';
import { InfoBubble } from './ui';
import { followExternal } from '../lib/external';

/**
 * AppTile is one way to get KnightLoader, GlimStone's reference/react/AppTile
 * in this app's classes: a mark above a name, lit to the picker tile's grey
 * under the pointer. A link where it leads to a file or a listing, a button
 * where it does something on the page, and a quiet tile with a "Soon" badge
 * where the listing does not exist yet: a tile that neither links nor acts is
 * one that is still to come. `face` replaces the mark and the name, as the
 * APK tile does with its code to scan.
 */
export function AppTile({
  name,
  logo,
  href,
  onClick,
  hint,
  hintLabel,
  face,
  soonLabel,
}: {
  name: string;
  logo: ReactNode;
  href?: string;
  onClick?: () => void;
  /** The (i) in the tile's corner, for what the name cannot say. */
  hint?: ReactNode;
  /** The (i)'s accessible name, needed where `hint` is not a plain sentence. */
  hintLabel?: string;
  face?: ReactNode;
  /** The badge on a tile with neither `href` nor `onClick`. */
  soonLabel: string;
}) {
  const body = face ?? (
    <>
      <span className="flex h-14 w-14 shrink-0 items-center justify-center">{logo}</span>
      <span className="px-1 text-center text-xs font-medium leading-tight">{name}</span>
    </>
  );
  const soon = !href && !onClick;
  return (
    <div className="group relative">
      {soon ? (
        <div className={`${TILE} text-carbon-textMuted`} aria-disabled>
          <span className="flex h-14 w-14 shrink-0 items-center justify-center opacity-45">{logo}</span>
          <span className="px-1 text-center text-xs font-medium leading-tight">{name}</span>
        </div>
      ) : href ? (
        <a
          href={href}
          target="_blank"
          rel="noreferrer noopener"
          onClick={followExternal}
          aria-label={name}
          className={`${TILE} ${LIVE}`}
        >
          {body}
        </a>
      ) : (
        <button type="button" onClick={onClick} aria-label={name} className={`${TILE} ${LIVE}`}>
          {body}
        </button>
      )}
      {soon && (
        <span
          className="absolute end-1.5 top-1.5 rounded-[var(--radius-pill)] bg-carbon-surface3 px-1.5 py-0.5
            text-[11px] font-semibold leading-none text-carbon-textSub"
        >
          {soonLabel}
        </span>
      )}
      {/* A sibling of the tile, so pressing it starts nothing. onColor, so the
          (i) takes the lit tile's ink, where the muted grey would fade. */}
      {hint && (
        <span className="absolute end-1.5 top-1.5 text-carbon-textSub group-hover:text-carbon-tileHoverInk">
          <InfoBubble tip={hint} label={hintLabel} onColor />
        </span>
      )}
    </div>
  );
}

// The vendor marks keep their colours, since each has a part that stands out
// on the hover grey (check-tile-hover.mjs); the single-colour ones take a
// deepened value there through the glim-*-mark classes in index.css.
const TILE =
  'flex h-28 w-28 flex-col items-center justify-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 ' +
  'text-carbon-text no-underline';
const LIVE = 'transition-colors duration-150 group-hover:bg-carbon-tileHover group-hover:text-carbon-tileHoverInk';

/**
 * BrandMark injects a vendor's SVG as it is published, since gradients, `<use>`
 * references and kebab-case attributes are easy to break in a JSX port. Every
 * id in it carries a prefix of its own so two marks cannot collide.
 */
export function BrandMark({ svg, className = '' }: { svg: string; className?: string }) {
  return (
    <span
      className={`block h-full w-full [&>svg]:block [&>svg]:h-full [&>svg]:w-full ${className}`}
      aria-hidden
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}
