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
  fetchSiteLogins,
  reconnectSiteLogin,
  removeSiteLogin,
} from '../lib/api';
import { openExternal } from '../lib/external';
import { fmtDate } from '../lib/format';
import { useT } from '../lib/i18n';
import { message } from '../lib/intake';
import { type SignInWays, nextStep, signedIn, sourceNote, stateNote } from '../lib/siteLogins';
import { useToast } from '../lib/toast';
import { IconClose, IconExternalLink, IconTrash } from '../lib/icons';
import { Button, Modal } from './ui';
import { SignInHereButton } from './SignInBrowserWindow';
import { SettingRow } from '../pages/settings/controls';

// While nobody is signed in the window asks often, since a sign-in arrives the
// moment the browser has it.
const SIGNED_OUT_POLL_MS = 3000;

/**
 * SiteState is a site's state as a word behind a dot, green with a session in
 * use and red without one, as every status on the accounts page reads.
 */
export function SiteState({ site }: { site: SiteLogin }) {
  const { t } = useT();
  const note = stateNote(site);
  const [text, dot] = signedIn(site) ? ['text-statusOk', 'bg-statusOkSolid'] : ['text-statusFail', 'bg-statusFailSolid'];
  return (
    <span className={`inline-flex items-center gap-1.5 text-meta font-medium ${text}`}>
      <span className={`h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] ${dot}`} />
      {t(note.key, note.vars)}
    </span>
  );
}

/**
 * SiteLoginDialog is one site's window. onSignedIn hears a sign-in that
 * arrived while it was open. toSignIn opens it to sign in again over a
 * session the site no longer takes, as a failed download does.
 */
export function SiteLoginDialog({
  site: initial,
  ways,
  hue,
  toSignIn = false,
  onChanged,
  onSignedIn,
  onClose,
}: {
  site: SiteLogin;
  ways: SignInWays;
  hue?: number;
  toSignIn?: boolean;
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

  const from = sourceNote(site);
  const step = nextStep(site, ways);
  const signingIn = waiting || toSignIn;
  // A browser that still holds the sign-in of a removed site brings it back
  // in one step, which leaves the sign-in browser nothing to add.
  const reconnectable = waiting && site.disconnected && ways.readsBrowsers;

  return (
    <Modal
      title={t('siteLogins.dialogTitle', { site: site.name })}
      hint={t('siteLogins.hint')}
      hue={hue}
      onClose={onClose}
      // The way out opens the row and opening the site, the one step forward,
      // ends it. Signing out, which leaves the browser signed in, is offered
      // only once there is nothing to sign in to; an expired session is
      // forgotten from its row's menu.
      footer={
        <>
          <Button kind="secondary" labelled icon={<IconClose />} title={t('failure.close')} disabled={busy} onClick={onClose} />
          <span className="flex-1" />
          {!signingIn && (
            <Button kind="secondary" icon={<IconTrash width={16} height={16} />} disabled={busy} onClick={() => void signOut()}>
              {t('siteLogins.signOut')}
            </Button>
          )}
          {reconnectable && (
            <Button kind="secondary" disabled={busy} onClick={() => void reconnect()}>
              {t('siteLogins.reconnect')}
            </Button>
          )}
          {signingIn && !reconnectable && (
            <SignInHereButton site={site} hue={hue} disabled={busy} onChanged={(s) => show(s, false)} />
          )}
          {signingIn && (
            <Button icon={<IconExternalLink />} disabled={busy} onClick={openSite}>
              {t('siteLogins.open', { site: site.name })}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <SettingRow label={t('siteLogins.status')} hint={t('siteLogins.statusHint')}>
          <span className="flex flex-col items-end gap-0.5 text-end">
            <SiteState site={site} />
            {from && <span className="text-meta text-carbon-textMuted">{t(from.key, from.vars)}</span>}
          </span>
        </SettingRow>
        {step && <p className="text-sm text-carbon-textSub">{t(step.key, step.vars)}</p>}
        {site.expires && (
          <SettingRow label={t('siteLogins.expires')}>
            <span className="glim-num text-sm text-carbon-textSub">{fmtDate(site.expires)}</span>
          </SettingRow>
        )}
        {error && (
          <p dir="auto" className="text-xs text-statusWarn">
            {error}
          </p>
        )}
      </div>
    </Modal>
  );
}
