// An instance without a login password answers whoever reaches its port, the
// backups with every stored password included. The desktop build opens no
// port, so there only its own window gets that far.
import { useEffect, useState } from 'react';
import { fetchAuth } from './api';
import { isDesktop } from './desktop';
import type { TranslationKey } from './i18n';

/** useNoPassword reports whether this instance answers anyone who reaches its
 *  port. It reads the state when the caller mounts, so a password set a
 *  moment ago in another tab counts at the next opening. */
export function useNoPassword(): boolean {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (isDesktop()) return;
    let live = true;
    fetchAuth()
      .then((a) => {
        if (live) setOpen(a.enabled === false);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);
  return open;
}

/** passwordPath names the settings page where the password is set. */
export function passwordPath(t: (key: TranslationKey) => string): string {
  return [t('settings.title'), t('settings.nav.access')].join(' > ');
}
