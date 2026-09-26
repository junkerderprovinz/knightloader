import { useLayoutEffect, useRef, type MouseEvent, type ReactNode } from 'react';
import { followExternal } from '../lib/external';
import { InfoBubble } from './ui';

// A button in the shape of the README's download buttons, on the App page and
// the About card (GlimStone's "The App tab"), from its reference/react
// ReadmeButton. The first part carries the mark and the full name; the parts
// after it are segments that name only what differs, such as ARM64 beside
// Windows. The unit lights up as one in its brand's colour ("Brand tiles"). A
// link where a part leads to a file or a listing, a button where it does
// something on the page, and a quiet unit with "Soon" as its second line where
// the listing does not exist yet.

// The brands index.css carries a tile colour for, each with its class:
// GlimStone's own and the browsers this app offers its extension for.
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
  paypal: 'glim-tile-paypal',
  bitcoin: 'glim-tile-bitcoin',
  coffee: 'glim-tile-coffee',
  house: 'glim-tile-house',
  chrome: 'kl-tile-chrome',
  firefox: 'kl-tile-firefox',
} as const;

export interface ReadmePart {
  name: string;
  /** The second line, shown under the pointer. Without one the name stays in the middle. */
  sub?: string;
  href?: string;
  onClick?: () => void;
}

export function ReadmeButton({
  brand,
  parts,
  mark,
  markClass,
  art,
  hint,
  hintLabel,
  note,
  soonLabel,
  onLinkClick = followExternal,
}: {
  /** The brand whose colour the unit lights up in. */
  brand: keyof typeof TILES;
  /** The button, then its segments. */
  parts: [ReadmePart, ...ReadmePart[]];
  mark?: ReactNode;
  /** The class that paints the mark at rest, such as `glim-windows-mark`. It
   *  sits on the mark's box, where the lit unit's ink can override it. */
  markClass?: string;
  /** A vendor's own button artwork, which replaces the first part's mark and words. */
  art?: ReactNode;
  /** The "(i)" at the end of the unit, for what the name cannot say. */
  hint?: ReactNode;
  /** The (i)'s accessible name, needed where `hint` is not a plain sentence. */
  hintLabel?: string;
  /** Holds the second lines in view while one reports what it did, such as "Copied". */
  note?: boolean;
  /** The second line of a unit whose first part has neither `href` nor `onClick`. */
  soonLabel?: string;
  /** Runs on a click on a part's link; the desktop build opens it in the system browser. */
  onLinkClick?: (e: MouseEvent<HTMLAnchorElement>) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const soon = !parts[0].href && !parts[0].onClick;
  const words = parts.map((p) => `${p.name}\n${p.sub ?? ''}`).join('\n') + (soon ? soonLabel : '');
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root) return;
    const fit = () => fitReadmeText(root);
    fit();
    const observer = new ResizeObserver(fit);
    observer.observe(root);
    void document.fonts?.ready.then(fit);
    return () => observer.disconnect();
  }, [words]);

  const unit = ['glim-readme-btn-unit', 'group', TILES[brand]];
  if (parts.length > 1) unit.push('glim-readme-btn-group');
  if (note) unit.push('glim-readme-btn-unit--note');
  if (soon) unit.push('glim-readme-btn-soon');

  return (
    <div ref={ref} className={unit.join(' ')}>
      {parts.map((part, i) => {
        const sub = soon ? soonLabel : part.sub;
        const label = sub ? `${part.name} ${sub}` : part.name;
        const className = `glim-readme-btn${i > 0 ? ' glim-readme-btn-seg' : ''}${soon ? '' : ' glim-brand-tile'}`;
        const face =
          i === 0 && art ? (
            <span className="glim-readme-btn-art" aria-hidden>
              {art}
            </span>
          ) : (
            <>
              {i === 0 && mark && (
                <span className={`glim-readme-btn-mark ${markClass ?? ''}`} aria-hidden>
                  {mark}
                </span>
              )}
              <span className="glim-readme-btn-text">
                <span className="glim-readme-btn-name">{part.name}</span>
                {sub && <span className="glim-readme-btn-sub">{sub}</span>}
              </span>
            </>
          );
        if (soon) {
          return (
            <span key={i} className={className} aria-disabled aria-label={label}>
              {face}
            </span>
          );
        }
        if (part.href) {
          return (
            <a
              key={i}
              href={part.href}
              target="_blank"
              rel="noreferrer noopener"
              onClick={onLinkClick}
              aria-label={label}
              className={className}
            >
              {face}
            </a>
          );
        }
        return (
          <button key={i} type="button" onClick={part.onClick} aria-label={label} className={className}>
            {face}
          </button>
        );
      })}
      {!soon && <span className="glim-readme-btn-sheen" aria-hidden />}
      {/* A sibling of the buttons, so pressing it starts nothing. onColor, so
          the (i) takes the lit unit's ink through the hint's own colour. */}
      {hint && (
        <span className="glim-readme-btn-hint text-carbon-textSub">
          <InfoBubble tip={hint} label={hintLabel} onColor />
        </span>
      )}
    </div>
  );
}

/**
 * fitReadmeText shrinks a translation longer than its button instead of
 * cutting it off. A line in a hidden page measures nothing, so the button fits
 * it again when a resize shows it.
 */
export function fitReadmeText(root: HTMLElement): void {
  for (const line of root.querySelectorAll<HTMLElement>('.glim-readme-btn-name, .glim-readme-btn-sub')) {
    line.style.fontSize = '';
    if (line.clientWidth === 0) continue;
    const over = line.scrollWidth / line.clientWidth;
    if (over > 1) line.style.fontSize = `${parseFloat(getComputedStyle(line).fontSize) / over}px`;
  }
}

/**
 * BrandMark injects a vendor's SVG as it is published, since gradients, `<use>`
 * references and kebab-case attributes are easy to break in a JSX port. Every
 * id in it carries a prefix of its own so two marks cannot collide. `lit` is
 * the brand's single-colour mark, shown on the lit unit in place of one whose
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
