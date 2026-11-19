import { ProviderMark } from '../../../components/CatalogTiles';
import { LabelBadge } from '../../../components/ui';
import type { VpnConnState, VpnProvider } from '../../../lib/api';
import { catalogEntry } from '../../../lib/catalogs/catalog';
import { VPN_PROVIDERS } from '../../../lib/catalogs/vpnProviders';
import { IconShieldOn } from '../../../lib/glyphs';
import type { TranslationKey } from '../../../lib/i18n';

type T = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** VpnMark is the mark a provider's tile wears in the picker, or the VPN glyph for one without. */
export function VpnMark({ id, size }: { id: string; size: number }) {
  return <ProviderMark mark={catalogEntry(VPN_PROVIDERS, id)?.mark} glyph={<IconShieldOn />} size={size} />;
}

/** The own config is no brand, so its name is the interface's words. */
export const nameOf = (t: T, p: Pick<VpnProvider, 'id' | 'name'>): string =>
  p.id === 'custom' ? t('settings.vpn.custom') : p.name;

const STATE_WORD: Record<VpnConnState, TranslationKey> = {
  idle: 'settings.vpn.state.saved',
  connecting: 'settings.vpn.state.connecting',
  connected: 'settings.vpn.state.connected',
  failed: 'settings.vpn.state.failed',
};

const STATE_TONE = { idle: undefined, connecting: 'warn', connected: 'ok', failed: 'fail' } as const;

/** VpnStateBadge is a provider's state as a badge, the way an account's status reads. */
export function VpnStateBadge({ provider: p, t }: { provider: VpnProvider; t: T }) {
  return <LabelBadge label={t(STATE_WORD[p.state])} tone={STATE_TONE[p.state]} />;
}

const FAILURE: Record<string, TranslationKey> = {
  'vpn-config-invalid': 'settings.vpn.error.config',
  'vpn-login-refused': 'settings.vpn.error.login',
  'vpn-timeout': 'settings.vpn.error.timeout',
  'vpn-dropped': 'settings.vpn.error.dropped',
  'vpn-device-limit': 'settings.vpn.error.deviceLimit',
  'vpn-account-expired': 'settings.vpn.error.accountExpired',
  'vpn-location-unknown': 'settings.vpn.error.locationUnknown',
  'vpn-resume-failed': 'settings.vpn.error.resumeFailed',
};

/** failureOf words why a provider failed, in the reader's language where the code is known. */
export function failureOf(t: T, p: VpnProvider): string {
  const key = p.errorCode ? FAILURE[p.errorCode] : undefined;
  return key ? t(key, { name: nameOf(t, p) }) : (p.error ?? '');
}
