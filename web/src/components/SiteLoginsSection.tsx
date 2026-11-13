// The accounts page's card for the sites signed into through the browser
// (internal/sitelogin): a tile per site with its logo and who it signs in as,
// and a window per site to sign in, paste a cookies.txt or sign out. Signing
// in opens the site's own login page in the browser, and the extension hands
// the session over from there; the page never sees the session itself.
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  type SiteLogin,
  beginSiteLogin,
  checkSiteLogin,
  fetchSiteLogins,
  pasteSiteSession,
  removeSiteLogin,
} from '../lib/api';
import { openExternal } from '../lib/external';
import { fmtDate } from '../lib/format';
import { useT } from '../lib/i18n';
import { message } from '../lib/intake';
import { tileNote } from '../lib/siteLogins';
import { useToast } from '../lib/toast';
import { IconBack, IconClose, IconCookies, IconExternalLink, IconSave, IconTrash } from '../lib/icons';
import { BrandPicker, type Brand } from './BrandPicker';
import { Button, Field, Modal, SectionTitle, TextArea } from './ui';
import { SettingRow } from '../pages/settings/controls';

// The tiles follow the account health poll; a window waiting for a sign-in
// asks more often, since the session arrives the moment the extension sends it.
const POLL_MS = 30000;
const WAITING_POLL_MS = 3000;

/** useSiteLogins polls the sites for the accounts page. */
export function useSiteLogins(): { sites: SiteLogin[]; load: () => Promise<void>; put: (s: SiteLogin) => void } {
  const [sites, setSites] = useState<SiteLogin[]>([]);
  const load = useCallback(async () => {
    try {
      setSites(await fetchSiteLogins());
    } catch {
      // A missed poll keeps the previous tiles.
    }
  }, []);
  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [load]);
  const put = useCallback((s: SiteLogin) => setSites((cur) => cur.map((x) => (x.id === s.id ? s : x))), []);
  return { sites, load, put };
}

export function SiteLoginsSection({ hue }: { hue: number }) {
  const { t } = useT();
  const { sites, put } = useSiteLogins();
  const [open, setOpen] = useState<string | null>(null);
  const opened = sites.find((s) => s.id === open);

  const brands: Brand[] = sites.map((s) => {
    const note = tileNote(s);
    return { id: s.id, name: s.name, host: s.host, sub: t(note.key, note.vars), held: true, warn: note.warn };
  });

  return (
    <>
      <SectionTitle hint={t('siteLogins.hint')}>{t('siteLogins.title')}</SectionTitle>
      <BrandPicker brands={brands} none="" onPick={setOpen} />
      {opened && <SiteLoginDialog site={opened} hue={hue} onChanged={put} onClose={() => setOpen(null)} />}
    </>
  );
}

/**
 * SiteLoginDialog is one site's window, from its tile or from a failed
 * download that needs the sign-in again. onSignedIn hears a session that
 * arrived while the window was open.
 */
export function SiteLoginDialog({
  site: initial,
  hue,
  onChanged,
  onSignedIn,
  onClose,
}: {
  site: SiteLogin;
  hue?: number;
  onChanged?: (s: SiteLogin) => void;
  onSignedIn?: (s: SiteLogin) => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [site, setSite] = useState(initial);
  const [pasting, setPasting] = useState(false);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const heard = useRef({ onChanged, onSignedIn, onClose });
  heard.current = { onChanged, onSignedIn, onClose };

  const show = useCallback((next: SiteLogin, signedInNow: boolean) => {
    setSite(next);
    heard.current.onChanged?.(next);
    if (signedInNow && next.state !== 'signedOut' && next.state !== 'expired') {
      toast(t('siteLogins.signedInToast', { site: next.name }), 'ok');
      heard.current.onSignedIn?.(next);
      heard.current.onClose();
    }
  }, [t, toast]);

  // A fresh answer on opening, since the tile may hold one from minutes ago.
  useEffect(() => {
    if (initial.state === 'signedOut') return;
    void checkSiteLogin(initial.id).then((s) => show(s, false), () => {});
  }, [initial.id, initial.state, show]);

  useEffect(() => {
    if (!site.waiting) return;
    const timer = window.setInterval(() => {
      void fetchSiteLogins().then((all) => {
        const latest = all.find((s) => s.id === site.id);
        if (latest && !latest.waiting) show(latest, true);
      }, () => {});
    }, WAITING_POLL_MS);
    return () => window.clearInterval(timer);
  }, [site.id, site.waiting, show]);

  const run = async (work: () => Promise<SiteLogin>, signedInNow = false): Promise<boolean> => {
    setBusy(true);
    setError('');
    try {
      show(await work(), signedInNow);
      return true;
    } catch (e) {
      // The server's sentence says what to do, such as signing in first.
      setError(message(e).replace(/^(Error|ApiError):\s*/, ''));
      return false;
    } finally {
      setBusy(false);
    }
  };

  // The login page opens first, inside the click, where no popup blocker
  // stands in the way; the wait starts after.
  const signIn = () => {
    openExternal(site.loginUrl);
    void run(() => beginSiteLogin(site.id));
  };

  const signOut = async () => {
    if (await run(() => removeSiteLogin(site.id))) toast(t('siteLogins.signedOutToast', { site: site.name }), 'info');
  };

  const note = tileNote(site);
  const stored = site.state !== 'signedOut';
  const close = <Button kind="secondary" labelled icon={<IconClose />} title={t('failure.close')} disabled={busy} onClick={onClose} />;

  return (
    <Modal
      title={t('siteLogins.dialogTitle', { site: site.name })}
      hint={t('siteLogins.hint')}
      hue={hue}
      onClose={onClose}
      footer={
        pasting ? (
          <>
            <Button
              kind="secondary"
              labelled
              icon={<IconBack className="rtl:-scale-x-100" />}
              title={t('onboarding.back')}
              disabled={busy}
              onClick={() => setPasting(false)}
            />
            <span className="flex-1" />
            <Button
              icon={<IconSave />}
              disabled={busy || text.trim() === ''}
              onClick={() => void run(() => pasteSiteSession(site.id, text), true)}
            >
              {t('accounts.save')}
            </Button>
          </>
        ) : (
          <>
            {close}
            <span className="flex-1" />
            {stored && (
              <Button kind="secondary" icon={<IconTrash width={16} height={16} />} disabled={busy} onClick={() => void signOut()}>
                {t('siteLogins.signOut')}
              </Button>
            )}
            <Button kind="secondary" icon={<IconCookies />} disabled={busy} onClick={() => setPasting(true)}>
              {t('siteLogins.paste')}
            </Button>
            <Button icon={<IconExternalLink />} disabled={busy} onClick={signIn}>
              {t(site.state === 'expired' ? 'siteLogins.signInAgain' : 'siteLogins.signIn')}
            </Button>
          </>
        )
      }
    >
      <div className="flex flex-col gap-4">
        {pasting ? (
          <Field label={t('settings.resolvers.cookieText')} hint={t('siteLogins.pasteHint', { site: site.name })}>
            <TextArea
              rows={6}
              dir="ltr"
              spellCheck={false}
              autoFocus
              value={text}
              placeholder={t('settings.resolvers.cookieTextPlaceholder')}
              disabled={busy}
              onChange={(e) => setText(e.target.value)}
            />
          </Field>
        ) : (
          <>
            <SettingRow label={t('siteLogins.status')} hint={t('siteLogins.statusHint')}>
              <span className={`text-sm font-medium ${note.warn ? 'text-statusWarn' : 'text-carbon-text'}`}>
                {t(note.key, note.vars)}
              </span>
            </SettingRow>
            {site.expires && (
              <SettingRow label={t('siteLogins.expires')}>
                <span className="glim-num text-sm text-carbon-textSub">{fmtDate(site.expires)}</span>
              </SettingRow>
            )}
            {site.waiting && <p className="text-sm text-carbon-textSub">{t('siteLogins.waitingHint', { site: site.name })}</p>}
          </>
        )}
        {error && <p dir="auto" className="text-xs text-statusWarn">{error}</p>}
      </div>
    </Modal>
  );
}
