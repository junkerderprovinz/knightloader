import { useSyncExternalStore } from 'react';

/**
 * Below Tailwind's `md`, the width where the rail gives way to the bottom bar
 * and the settings rail shows its glyphs alone. Written as the last pixel under
 * 768 so it and the `md:` classes never both apply; index.css's
 * --phone-bar-space uses the same query.
 */
const PHONE = '(max-width: 767.98px)';

/**
 * Below Tailwind's `lg`. Beside the full sidebar, a settings rail with words
 * leaves the page a column too narrow for a selector's options to share a row.
 */
const TABLET = '(max-width: 1023.98px)';

/** watch turns one media query into what useSyncExternalStore takes. */
function watch(query: string) {
  return {
    subscribe(onChange: () => void): () => void {
      const list = window.matchMedia(query);
      list.addEventListener('change', onChange);
      return () => list.removeEventListener('change', onChange);
    },
    matches: () => window.matchMedia(query).matches,
  };
}

const phone = watch(PHONE);
const tablet = watch(TABLET);

/** usePhoneLayout says whether the window is narrow enough for the phone layout. */
export function usePhoneLayout(): boolean {
  return useSyncExternalStore(phone.subscribe, phone.matches, () => false);
}

/** useTabletLayout says whether the window is below `lg`, phone widths included. */
export function useTabletLayout(): boolean {
  return useSyncExternalStore(tablet.subscribe, tablet.matches, () => false);
}
