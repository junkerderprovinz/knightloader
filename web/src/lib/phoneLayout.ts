import { useSyncExternalStore } from 'react';

/**
 * Below Tailwind's `md`, the width where the rail gives way to the bottom bar
 * and the settings rail shows its glyphs alone. Written as the last pixel under
 * 768 so it and the `md:` classes never both apply; index.css's
 * --phone-bar-space uses the same query.
 */
const PHONE = '(max-width: 767.98px)';

function subscribe(onChange: () => void): () => void {
  const query = window.matchMedia(PHONE);
  query.addEventListener('change', onChange);
  return () => query.removeEventListener('change', onChange);
}

const isPhone = () => window.matchMedia(PHONE).matches;

/** usePhoneLayout says whether the window is narrow enough for the phone layout. */
export function usePhoneLayout(): boolean {
  return useSyncExternalStore(subscribe, isPhone, () => false);
}
