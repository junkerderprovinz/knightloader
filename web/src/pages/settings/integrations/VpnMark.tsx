import { LabelBadge } from '../../../components/ui';
import { Logo, Monogram, monogramPaint, type Paint } from '../../../components/HosterIcon';
import type { VpnConnState, VpnProvider } from '../../../lib/api';
import { withBase } from '../../../lib/basePath';
import type { TranslationKey } from '../../../lib/i18n';
import type { LogoTile } from '../../../lib/logoTile';
import { VPN_APP_ONLY, VPN_PROVIDERS } from '../../../lib/vpnProviders';

type T = (key: TranslationKey, vars?: Record<string, string | number>) => string;

const LOGOS = new Map<string, LogoTile | null>([...VPN_PROVIDERS, ...VPN_APP_ONLY].map((p) => [p.id, p.logo]));

/**
 * VpnMark is a provider's logo from public/vpn, or its letter tile where there
 * is none. `lit` adds the mask its brand tile shows under the pointer.
 */
export function VpnMark({ id, name, size, lit }: { id: string; name: string; size: number; lit?: boolean }) {
  const box = { width: size, height: size };
  const logo = LOGOS.get(id);
  if (!logo) return <Monogram name={name} box={box} />;
  const src = withBase(`/vpn/${id}.png`);
  const mask = logo.lit ? withBase(`/vpn/${id}.lit.png`) : src;
  return <Logo src={src} mask={mask} info={logo} box={box} lit={lit} />;
}

/** vpnPaint is the colours a provider's tile lights up in. */
export const vpnPaint = (id: string, name: string): Paint => LOGOS.get(id) ?? monogramPaint(name);

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
