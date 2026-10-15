import { LabelBadge } from '../../../components/ui';
import { Monogram } from '../../../components/HosterIcon';
import type { VpnConnState, VpnProvider } from '../../../lib/api';
import { withBase } from '../../../lib/basePath';
import type { TranslationKey } from '../../../lib/i18n';
import { VPN_APP_ONLY, VPN_PROVIDERS } from '../../../lib/vpnProviders';

type T = (key: TranslationKey, vars?: Record<string, string | number>) => string;

const WITH_LOGO = new Set([...VPN_PROVIDERS, ...VPN_APP_ONLY].filter((p) => p.logo).map((p) => p.id));

/** VpnMark is a provider's logo from public/vpn, or its letter tile where there is none. */
export function VpnMark({ id, name, size }: { id: string; name: string; size: number }) {
  const box = { width: size, height: size };
  if (!WITH_LOGO.has(id)) return <Monogram name={name} box={box} />;
  return (
    <img
      src={withBase(`/vpn/${id}.png`)}
      alt=""
      aria-hidden
      style={box}
      className="inline-block shrink-0 rounded-[var(--radius-control)] object-contain"
    />
  );
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
};

/** failureOf words why a provider failed, in the reader's language where the code is known. */
export function failureOf(t: T, p: VpnProvider): string {
  const key = p.errorCode ? FAILURE[p.errorCode] : undefined;
  return key ? t(key, { name: nameOf(t, p) }) : (p.error ?? '');
}
