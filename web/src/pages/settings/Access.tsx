import { useEffect, useMemo, useState } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { SignInTile } from '@glimstone/react';
import { takeSignInCode, type ToastSeverity } from '@glimstone/core';
import { IDENTITY_PROVIDERS } from '@glimstone/core/identityProviders';
import { Card, Field, LinkBadge } from '../../components/ui';
import { fetchAccess, type AccessState } from '../../lib/api';
import { withBase } from '../../lib/basePath';
import { fmtDate } from '../../lib/format';
import { useT } from '../../lib/i18n';
import { reauthClient } from '../../lib/signin/kit';
import { addressPath } from '../../lib/signin/path';
import { TOKEN_LEVELS, words } from '../../lib/signin/words';
import { useToast, type ToastTone } from '../../lib/toast';
import { unusedProxyRanges } from '../../lib/trustedProxies';
import { useDraft } from './context';
import { ListArea, RowRefusal, SettingRow } from './controls';
import { ModuleToggle } from './ModuleToggle';

// The manual's section on the encryption key, which says how to make one and move it.
const APP_KEY_MANUAL = 'https://junkerderprovinz.github.io/knightloader/installing/#encryption-key';

const TONE: Record<ToastSeverity, ToastTone> = { success: 'ok', warn: 'info', fail: 'fail' };

export function Access() {
  const { t, lang } = useT();
  const { toast } = useToast();
  // A test sign-in comes back here with what the sign-in service said.
  const [arrived] = useState(takeSignInCode);
  const text = useMemo(() => words(t, lang).tileText(addressPath(t)), [t, lang]);

  // The pairing section lived on this page, so an old link to it lands
  // on the page it has moved to.
  if (useLocation().hash === '#pairing') return <Navigate to="/settings/pairing" replace />;

  return (
    <div className="flex flex-col gap-10">
      <SignInTile
        api={reauthClient}
        text={text}
        lang={lang}
        providers={IDENTITY_PROVIDERS}
        scopes={[...TOKEN_LEVELS]}
        notify={(message, severity) => toast(message, TONE[severity])}
        returnTo={withBase('/settings/access')}
        arrived={arrived}
        time={(iso) => fmtDate(iso)}
      />
      <KeyAndProxies />
    </div>
  );
}

/**
 * KeyAndProxies holds what guards the instance besides the sign-in: the
 * encryption key, the reverse proxies whose addresses count, and the door
 * Sonarr and Radarr use with an API token.
 */
function KeyAndProxies() {
  const { t } = useT();
  const [access, setAccess] = useState<AccessState | null>(null);

  useEffect(() => {
    fetchAccess()
      .then(setAccess)
      .catch(() => setAccess(null));
  }, []);

  return (
    <Card hue={5} className="flex flex-col gap-5">
      {/* Everything works without an encryption key, so it is a name with its
          explanation behind the (i) rather than a warning or a state. */}
      <div data-testid="app-key">
        <SettingRow label={t('settings.appKeyTitle')} hint={t('settings.appKeyHint')}>
          <LinkBadge href={APP_KEY_MANUAL} title={t('settings.appKeyManual')} />
        </SettingRow>
      </div>
      {access?.appKeyCopiesUntil && (
        <span className="text-sm text-carbon-textSub">
          {t('settings.appKeyCopies', { date: fmtDate(access.appKeyCopiesUntil) })}
        </span>
      )}
      <TrustedProxiesField behindProxy={access?.behindProxy ?? false} />
      <ModuleToggle id="downloadclient" hue={5} hint={t('settings.modules.about.downloadclient')} />
    </Card>
  );
}

/**
 * TrustedProxiesField names the reverse proxies whose X-Forwarded-For the
 * sign-in reads for the client address. It saves with the settings, and shows
 * only once a proxy is in front or the list has entries.
 */
function TrustedProxiesField({ behindProxy }: { behindProxy: boolean }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  // Once shown, the field stays while its last line is deleted.
  const [kept, setKept] = useState(false);
  const wanted = behindProxy || cfg.trustedProxies.length > 0;
  if (wanted && !kept) setKept(true);
  const unused = unusedProxyRanges(cfg.trustedProxies);
  if (!wanted && !kept) return null;
  return (
    <div className="flex flex-col gap-1.5">
      <Field label={t('settings.access.trustedProxies')} hint={t('settings.access.trustedProxiesHint')}>
        <ListArea rows={3} dir="ltr" lines={cfg.trustedProxies} onLines={(trustedProxies) => patch({ trustedProxies })} />
      </Field>
      {unused.length > 0 && (
        <p className="text-xs text-statusWarn">
          {t('settings.access.trustedProxiesUnused', { entries: unused.join(', ') })}
        </p>
      )}
      <RowRefusal field="trustedProxies" className="pb-0" />
    </div>
  );
}
