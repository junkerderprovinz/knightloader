// The window of one site signed into through the browser (internal/sitelogin),
// opened from its row on the hoster card and from a failed download. Signing
// in happens on the site, in the browser: an instance outside a container
// reads the session from there and the extension passes it on, so the window
// only says how it stands and notices a sign-in by itself.
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  type SiteLogin,
  beginSiteLogin,
  checkSiteLogin,
  fetchConnect,
  fetchSiteLogins,
  reconnectSiteLogin,
  removeSiteLogin,
} from '../lib/api';
import { openExternal } from '../lib/external';
import { fmtDate } from '../lib/format';
import { useT } from '../lib/i18n';
import { message } from '../lib/intake';
import { type SignInWays, nextStep, readsBrowsers, signedIn, sourceNote, stateNote } from '../lib/siteLogins';
import { useToast } from '../lib/toast';
import { IconClose, IconExternalLink, IconTrash } from '../lib/icons';
import { Button, Modal } from './ui';
import { SettingRow } from '../pages/settings/controls';

// While nobody is signed in the window asks often, since a sign-in arrives the
// moment the browser has it.
const SIGNED_OUT_POLL_MS = 3000;

/**
 * useSignInWays says where this instance gets a sign-in from: outside a
 * container it reads the browsers on its computer, and a container needs a
 * browser of its group that has the extension.
 */
export function useSignInWays(sites: SiteLogin[], enabled = true): SignInWays {
  const [extensionPaired, setExtensionPaired] = useState(false);
  useEffect(() => {
    if (!enabled) return;
    void fetchConnect().then(
      (c) => setExtensionPaired((c.apps ?? []).some((a) => a.deployment === 'extension' && a.approved)),
      () => {},
    );
  }, [enabled]);
  return { readsBrowsers: readsBrowsers(sites), extensionPaired };
}

/** SiteLoginDialog is one site's window. onSignedIn hears a sign-in that arrived while it was open. */
export function SiteLoginDialog({
  site: initial,
  ways,
  hue,
  onChanged,
  onSignedIn,
  onClose,
}: {
  site: SiteLogin;
  ways: SignInWays;
  hue?: number;
  onChanged?: (s: SiteLogin) => void;
  onSignedIn?: (s: SiteLogin) => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [site, setSite] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const heard = useRef({ onChanged, onSignedIn, onClose });
  heard.current = { onChanged, onSignedIn, onClose };

  const show = useCallback(
    (next: SiteLogin, arrived: boolean) => {
      setSite(next);
      heard.current.onChanged?.(next);
      if (arrived) {
        toast(t('siteLogins.signedInToast', { site: next.name }), 'ok');
        heard.current.onSignedIn?.(next);
        heard.current.onClose();
      }
    },
    [t, toast],
  );

  // A fresh answer on opening, since the row may hold one from minutes ago.
  useEffect(() => {
    if (initial.state === 'signedOut') return;
    void checkSiteLogin(initial.id).then((s) => show(s, false), () => {});
  }, [initial.id, initial.state, show]);

  const waiting = !signedIn(site);
  useEffect(() => {
    if (!waiting) return;
    const timer = window.setInterval(() => {
      void fetchSiteLogins().then((all) => {
        const latest = all.find((s) => s.id === site.id);
        if (latest && signedIn(latest)) show(latest, true);
      }, () => {});
    }, SIGNED_OUT_POLL_MS);
    return () => window.clearInterval(timer);
  }, [site.id, waiting, show]);

  const showError = (e: unknown) => setError(message(e).replace(/^(Error|ApiError):\s*/, ''));

  const signOut = async () => {
    setBusy(true);
    setError('');
    try {
      show(await removeSiteLogin(site.id), false);
      toast(t('siteLogins.signedOutToast', { site: site.name }), 'info');
    } catch (e) {
      showError(e);
    } finally {
      setBusy(false);
    }
  };

  const reconnect = async () => {
    setBusy(true);
    setError('');
    try {
      const next = await reconnectSiteLogin(site.id);
      show(next, signedIn(next));
    } catch (e) {
      showError(e);
    } finally {
      setBusy(false);
    }
  };

  // The site opens first, while the click still lets it. A removed site is
  // signed in through KnightLoader too, so the browser's sign-in counts again.
  const openSite = () => {
    openExternal(site.loginUrl);
    if (site.disconnected) void beginSiteLogin(site.id).then((next) => show(next, signedIn(next)), showError);
  };

  const note = stateNote(site);
  const from = sourceNote(site);
  const step = nextStep(site, ways);
  // Signing out here leaves the browser signed in, whatever the session came from.
  const forgettable = site.state !== 'signedOut';

  return (
    <Modal
      title={t('siteLogins.dialogTitle', { site: site.name })}
      hint={t('siteLogins.hint')}
      hue={hue}
      onClose={onClose}
      footer={
        <>
          <Button kind="secondary" labelled icon={<IconClose />} title={t('failure.close')} disabled={busy} onClick={onClose} />
          <span className="flex-1" />
          {forgettable && (
            <Button kind="secondary" icon={<IconTrash width={16} height={16} />} disabled={busy} onClick={() => void signOut()}>
              {t('siteLogins.signOut')}
            </Button>
          )}
          {site.disconnected && ways.readsBrowsers && (
            <Button kind="secondary" disabled={busy} onClick={() => void reconnect()}>
              {t('siteLogins.reconnect')}
            </Button>
          )}
          <Button kind={waiting ? 'primary' : 'secondary'} icon={<IconExternalLink />} disabled={busy} onClick={openSite}>
            {t('siteLogins.open', { site: site.name })}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <SettingRow label={t('siteLogins.status')} hint={t('siteLogins.statusHint')}>
          <span className="flex flex-col items-end gap-0.5 text-end">
            <span className={`text-sm font-medium ${note.warn ? 'text-statusWarn' : 'text-carbon-text'}`}>{t(note.key, note.vars)}</span>
            {from && <span className="text-meta text-carbon-textMuted">{t(from.key, from.vars)}</span>}
          </span>
        </SettingRow>
        {site.expires && (
          <SettingRow label={t('siteLogins.expires')}>
            <span className="glim-num text-sm text-carbon-textSub">{fmtDate(site.expires)}</span>
          </SettingRow>
        )}
        {step && <p className="text-sm text-carbon-textSub">{t(step.key, step.vars)}</p>}
        {error && (
          <p dir="auto" className="text-xs text-statusWarn">
            {error}
          </p>
        )}
      </div>
    </Modal>
  );
}
