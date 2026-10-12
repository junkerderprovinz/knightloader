// A picker of brands (GlimStone, "Pickers and wizards"): one window, every
// provider a tile in the shape of the README's buttons with its logo at the
// start and its name beside it, and a search at the window's top right. The
// debrid services, the captcha services and the hosters are all picked from
// it, so the three windows look the same. The tile is ReadmeButton's markup
// without its fitting of long words, which several hundred hosters would each
// pay for: a brand's name is cut with an ellipsis instead.
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useT } from '../lib/i18n';
import { IconClose, IconSearch } from '../lib/icons';
import { HosterIcon } from './HosterIcon';
import { IconBadge } from './ui';

export interface Brand {
  id: string;
  name: string;
  /** The second line, shown under the pointer: the brand's site, or that it is connected. */
  sub?: string;
  /** The host or address the logo is fetched for. */
  host: string;
  /** Keeps the second line in view at rest, for one that says a state. */
  held?: boolean;
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
      {brands.map((b) => (
        <li
          key={b.id}
          className={`glim-readme-btn-unit group glim-tile-house ${b.held ? 'glim-readme-btn-unit--note' : ''}`}
        >
          <button
            type="button"
            onClick={() => onPick(b.id)}
            aria-label={b.sub ? `${b.name} ${b.sub}` : b.name}
            className="glim-readme-btn glim-brand-tile"
          >
            <span className="glim-readme-btn-mark" aria-hidden>
              {/* Lazy, so only tiles scrolled into view fetch their logo. */}
              <HosterIcon host={b.host} size={25} />
            </span>
            <span className="glim-readme-btn-text">
              <span className="glim-readme-btn-name">{b.name}</span>
              {b.sub && <span className="glim-readme-btn-sub">{b.sub}</span>}
            </span>
          </button>
          <span className="glim-readme-btn-sheen" aria-hidden />
        </li>
      ))}
    </ul>
  );
}

/** One kind of thing a picker of kinds offers. */
export interface Kind {
  id: string;
  name: string;
  /** What the kind is for, in a few words, always in view. */
  sub: string;
  icon: ReactNode;
}

/**
 * KindPicker is the step before a picker of brands, where there is a kind of
 * thing to choose first: the same tile a size up, a glyph in place of the
 * logo, and the second line held in view, since a handful of kinds is read
 * rather than searched.
 */
export function KindPicker({ kinds, onPick }: { kinds: Kind[]; onPick: (id: string) => void }) {
  return (
    <ul data-new="account-kinds" className="glim-provider-pick glim-kind-pick">
      {kinds.map((k) => (
        <li key={k.id} className="glim-readme-btn-unit glim-readme-btn-unit--note group glim-tile-house">
          <button
            type="button"
            onClick={() => onPick(k.id)}
            aria-label={`${k.name} ${k.sub}`}
            className="glim-readme-btn glim-brand-tile"
          >
            <span className="glim-readme-btn-mark glim-house-mark" aria-hidden>
              {k.icon}
            </span>
            <span className="glim-readme-btn-text">
              <span className="glim-readme-btn-name">{k.name}</span>
              <span className="glim-readme-btn-sub">{k.sub}</span>
            </span>
          </button>
          <span className="glim-readme-btn-sheen" aria-hidden />
        </li>
      ))}
    </ul>
  );
}

/**
 * PickerSearch is the search of a picker's window: a button that turns into
 * its field when pressed. `open` starts it as the field, for a list too long
 * to pick from without typing.
 */
export function PickerSearch({
  value,
  onChange,
  placeholder,
  open = false,
}: {
  value: string;
  onChange: (next: string) => void;
  placeholder: string;
  open?: boolean;
}) {
  const { t } = useT();
  const [shown, setShown] = useState(open);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (shown) input.current?.focus();
  }, [shown]);

  if (!shown) {
    return (
      <IconBadge labelled icon={<IconSearch width={16} height={16} />} title={t('search.toggle')} onClick={() => setShown(true)} />
    );
  }
  return (
    <div
      className="flex h-[var(--btn-h)] w-56 max-w-full items-center gap-1 rounded-[var(--radius-control)] bg-carbon-surface2
        pe-1 ps-2.5 transition-shadow has-[input:focus]:shadow-[0_0_0_2px_var(--focus-ring)]"
    >
      <IconSearch className="shrink-0 text-carbon-textMuted" width={15} height={15} />
      <input
        ref={input}
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        className="glim-focus-drawn h-full min-w-0 flex-1 bg-transparent text-sm text-carbon-text
          placeholder:text-carbon-textMuted outline-none [&::-webkit-search-cancel-button]:hidden"
      />
      {value && (
        <button
          type="button"
          aria-label={t('search.clear')}
          onClick={() => {
            onChange('');
            input.current?.focus();
          }}
          className="grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-pill)]
            text-carbon-textMuted transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
        >
          <IconClose width={12} height={12} />
        </button>
      )}
    </div>
  );
}
