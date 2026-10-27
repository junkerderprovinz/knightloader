// An instance without a login password answers whoever reaches its port, so
// it refuses backups until one is set (internal/api/password_first.go). The
// desktop build opens no port, and its window is never refused.
import { useEffect, useState } from 'react';
import { fetchAuth } from './api';
import { isDesktop } from './desktop';
import type { TranslationKey } from './i18n';

/** usePasswordFirst reports whether this instance refuses those until a
 *  password is set. It reads the state when the caller mounts, so a password
 *  set a moment ago in another tab counts at the next opening. */
export function usePasswordFirst(): boolean {
  const [locked, setLocked] = useState(false);
  useEffect(() => {
    if (isDesktop()) return;
    let live = true;
    fetchAuth()
      .then((a) => {
        if (live) setLocked(a.enabled === false);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);
  return locked;
}

/** passwordPath names the settings page where the password is set. */
export function passwordPath(t: (key: TranslationKey) => string): string {
  return [t('settings.title'), t('settings.nav.access')].join(' > ');
}
