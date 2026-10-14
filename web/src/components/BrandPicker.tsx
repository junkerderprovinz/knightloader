// A picker of brands (GlimStone, "Pickers and wizards"): one window, every
// provider a tile in the shape of the README's buttons with its logo at the
// start and its name beside it, and a search at the window's top right. The
// debrid services, the captcha services, the hosters, the VPN providers and the
// MCP clients are all picked from it, so their windows look the same. The tile is ReadmeButton's markup
// without its fitting of long words, which several hundred hosters would each
// pay for: a brand's name is cut with an ellipsis instead.
import type { ReactNode } from 'react';
import { HosterIcon } from './HosterIcon';
import { SearchBox } from './SearchBox';
import { InfoBubble } from './ui';

export interface Brand {
  id: string;
  name: string;
  /** The second line, shown under the pointer: the brand's site, or that it is connected. */
  sub?: string;
  /** The host or address the logo is fetched for, where the brand brings no mark of its own. */
  host?: string;
  /** Keeps the second line in view at rest, for one that says a state. */
  held?: boolean;
  /** A logo of the page's own in place of the one fetched for host. */
  mark?: ReactNode;
  /** The `.glim-tile-*` class the tile lights up in; the accent without one. */
  tile?: string;
  /** Why the brand cannot be picked, behind an (i) on a greyed-out tile. */
  unavailable?: string;
}

/** Whether a brand's name holds what was typed. */
export const brandMatches = (b: Brand, query: string): boolean =>
  b.name.toLowerCase().includes(query.trim().toLowerCase());

/** siteOf is the site an address belongs to, as a second line: its host without "www". */
export function siteOf(address: string): string {
  try {
    return new URL(address).hostname.replace(/^www\./, '');
  } catch {
    return '';
  }
}

export function BrandPicker({
  brands,
  onPick,
  none,
}: {
  brands: Brand[];
  onPick: (id: string) => void;
  /** What to say when the search leaves no brand. */
  none: string;
}) {
  if (brands.length === 0) return <p className="px-2 py-6 text-center text-sm text-carbon-textMuted">{none}</p>;
  return (
    <ul data-new="account-picker" className="glim-provider-pick">
      {brands.map((b) => {
        const face = (
          <>
            <span className="glim-readme-btn-mark" aria-hidden>
              {/* Lazy, so only tiles scrolled into view fetch their logo. */}
              {b.mark ?? <HosterIcon host={b.host ?? ''} size={25} />}
            </span>
            <span className="glim-readme-btn-text">
              <span className="glim-readme-btn-name">{b.name}</span>
              {b.sub && <span className="glim-readme-btn-sub">{b.sub}</span>}
            </span>
          </>
        );
        const label = b.sub ? `${b.name} ${b.sub}` : b.name;
        if (b.unavailable) {
          return (
            <li key={b.id} className={`glim-readme-btn-unit group ${b.tile ?? 'glim-tile-house'} glim-readme-btn-soon`}>
              <span className="glim-readme-btn" aria-disabled aria-label={label}>
                {face}
              </span>
              <span className="glim-readme-btn-hint text-carbon-textSub">
                <InfoBubble tip={b.unavailable} />
              </span>
            </li>
          );
        }
        return (
          <li
            key={b.id}
            className={`glim-readme-btn-unit group ${b.tile ?? 'glim-tile-house'} ${b.held ? 'glim-readme-btn-unit--note' : ''}`}
          >
            <button type="button" onClick={() => onPick(b.id)} aria-label={label} className="glim-readme-btn glim-brand-tile">
              {face}
            </button>
            <span className="glim-readme-btn-sheen" aria-hidden />
          </li>
        );
      })}
    </ul>
  );
}

/**
 * PickerSearch is the search of a picker's window, a field from the start
 * since searching is what the window is mostly for. It takes the caret where
 * there is a pointer; under a finger the keyboard would cover the tiles.
 */
export function PickerSearch({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder: string;
}) {
  return (
    <div data-new="picker-search-field" className="glim-picker-search w-64 max-w-full">
      <SearchBox value={value} onChange={onChange} label={placeholder} autoFocus={!matchMedia('(hover: none)').matches} />
    </div>
  );
}
