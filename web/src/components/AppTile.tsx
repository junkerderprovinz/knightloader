import type { ReactNode } from 'react';
import { InfoBubble } from './ui';
import { followExternal } from '../lib/external';

// The brands index.css carries a tile colour for, each with its class: GlimStone's
// own and the browsers this app offers its extension for.
const TILES = {
  windows: 'glim-tile-windows',
  apple: 'glim-tile-apple',
  linux: 'glim-tile-linux',
  android: 'glim-tile-android',
  play: 'glim-tile-play',
  docker: 'glim-tile-docker',
  unraid: 'glim-tile-unraid',
  zip: 'glim-tile-zip',
  github: 'glim-tile-github',
  chrome: 'kl-tile-chrome',
  edge: 'kl-tile-edge',
  brave: 'kl-tile-brave',
  opera: 'kl-tile-opera',
  vivaldi: 'kl-tile-vivaldi',
  firefox: 'kl-tile-firefox',
} as const;

/**
 * AppTile is one way to get KnightLoader, GlimStone's reference/react/AppTile
 * in this app's classes: a mark above a name, lighting up in its brand's colour
 * under the pointer. A link where it leads to a file or a listing, a button
 * where it does something on the page, and a quiet tile with a "Soon" badge
 * where the listing does not exist yet: a tile that neither links nor acts is
 * one that is still to come. `face` replaces the mark and the name, as the
 * APK tile does with its code to scan.
 */
export function AppTile({
  name,
  logo,
  brand,
  href,
  onClick,
  hint,
  hintLabel,
  face,
  soonLabel,
}: {
  name: string;
  logo: ReactNode;
  /** The brand whose colour the tile lights up in. */
  brand: keyof typeof TILES;
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
    <div className={`group relative ${TILES[brand]}`}>
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
          className={`${TILE} glim-brand-tile`}
        >
          {body}
        </a>
      ) : (
        <button type="button" onClick={onClick} aria-label={name} className={`${TILE} glim-brand-tile`}>
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
        <span className="absolute end-1.5 top-1.5 text-carbon-textSub group-hover:text-[var(--tile-ink)]">
          <InfoBubble tip={hint} label={hintLabel} onColor />
        </span>
      )}
    </div>
  );
}

const TILE =
  'flex h-28 w-28 flex-col items-center justify-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 ' +
  'text-carbon-text no-underline';

/**
 * BrandMark injects a vendor's SVG as it is published, since gradients, `<use>`
 * references and kebab-case attributes are easy to break in a JSX port. Every
 * id in it carries a prefix of its own so two marks cannot collide. `lit` is
 * the brand's single-colour mark, shown on the lit tile in place of one whose
 * layers do not survive a single ink.
 */
export function BrandMark({ svg, lit, className = '' }: { svg: string; lit?: string; className?: string }) {
  const box = `block h-full w-full [&>svg]:block [&>svg]:h-full [&>svg]:w-full ${className}`;
  if (!lit) return <span className={box} aria-hidden dangerouslySetInnerHTML={{ __html: svg }} />;
  return (
    <>
      <span className={`glim-mark-rest ${box}`} aria-hidden dangerouslySetInnerHTML={{ __html: svg }} />
      <span className={`glim-mark-hover ${box}`} aria-hidden dangerouslySetInnerHTML={{ __html: lit }} />
    </>
  );
}
