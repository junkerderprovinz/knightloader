import type { Translate } from './words';

/** addressPath names where KnightLoader's own address is set, in the page's words. */
export function addressPath(t: Translate): string {
  return [t('settings.title'), t('settings.nav.pairing'), t('settings.access.identity.addressesLabel')].join(' > ');
}
