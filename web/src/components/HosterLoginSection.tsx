// Hoster logins for the headless JD sidecar (see internal/hosterauth). Saving
// writes the credential into JD's account config through its Remote API, and
// JD's hoster plugin performs the login; JD's own UI is never shown. Each login
// is a connection row that says what JD makes of it, and this file adds the
// host picker. The picker offers no debrid service: KnightLoader speaks to
// each of those itself. The card's Add button on the accounts page opens the
// picker through `adding`.
//
// The sites signed into through the browser (internal/sitelogin) are added
// from the same picker and stand under the logins: each says who it signs in
// as and where from, or how to sign in, tests its session, signs in again in
// the sign-in browser, and opens SiteLoginDialog rather than a password form.
import { pageLang } from '../lib/picker';
import { type ReactNode, useCallback, useEffect, useRef, useState } from 'react';
import {
  type HosterHost,
  type HosterLogin,
  type SiteLogin,
  addSiteLogin,
  checkSiteLogin,
  fetchHosterHosts,
  fetchHosterLogins,
  fetchSiteLogins,
  removeHosterLogin,
  removeSiteLogin,
  saveHosterLogin,
  setHosterLoginEnabled,
} from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { fmtDate } from '../lib/format';
import { type SignInWays, nextStep, siteForHost, signedIn, sourceNote, stateNote, readsBrowsers, unreadableBrowsers } from '../lib/siteLogins';
import { useToast } from '../lib/toast';
import { Button, EmptyState, LabelBadge, Modal, TextInput } from './ui';
import { ConnectionList, ServiceConnection, TrafficLine, planLine, switchAction } from './AccountConnection';
import { IconAccounts, IconBack, IconCheck, IconClose, IconSave, IconSignIn, IconTrash } from '../lib/icons';
import { BrandPicker, PickerSearch, brandMatches, type Brand } from './BrandPicker';
import { catalogBrands } from './CatalogTiles';
import { ownTile } from './OwnLogos';
import { DOWNLOAD_PROVIDERS, type DownloadProviderId } from '../lib/catalogs/downloadProviders';
import { catalogEntry } from '../lib/catalogs/catalog';
import { IconCloud } from '../lib/glyphs';
import { SiteLoginDialog } from './SiteLoginDialog';
import type { ConnectionTest } from './ConnectionRow';
import { SignInBrowserWindow, SignInHereButton, useSignInBrowserAvailable } from './SignInBrowserWindow';
import { SettingRow } from '../pages/settings/controls';

// SITE_BRAND sets a site's tile in the picker apart from a hoster of the same
// name.
const SITE_BRAND = 'site:';

// Faster than the 30s account health poll, since a new login moves from queued
// to active or rejected within seconds to minutes.
const POLL_MS = 8000;

// The login window follows a login JD is still checking more closely, since
// somebody is looking at it.
const TEST_POLL_MS = 2000;

/** HosterLogins is the logins, the sites signed into through the browser and the host list. */
export interface HosterLogins {
  logins: HosterLogin[] | null;
  sites: SiteLogin[];
  /** Where this instance can get a sign-in from, which the sites' rows tell. */
  ways: SignInWays;
  hosts: HosterHost[];
  load: () => Promise<void>;
  toggle: (row: HosterLogin, enabled: boolean) => Promise<void>;
  toggleAll: (enabled: boolean) => Promise<void>;
  remove: (host: string) => Promise<void>;
  putSite: (site: SiteLogin) => void;
  /** addSite gives a site its row and answers with it, or with nothing when that failed. */
  addSite: (site: SiteLogin) => Promise<SiteLogin | undefined>;
  removeSite: (site: SiteLogin) => Promise<void>;
}

/**
 * useHosterLogins polls the logins for the accounts page. onEnabledHosts hears
 * the switched-on hosts after every load, since each is a row on the priority
 * card.
 */
export function useHosterLogins(onEnabledHosts?: (hosts: string) => void): HosterLogins {
  const { t } = useT();
  const { toast } = useToast();
  const [logins, setLogins] = useState<HosterLogin[] | null>(null);
  const [sites, setSites] = useState<SiteLogin[]>([]);
  const [hosts, setHosts] = useState<HosterHost[]>([]);

  const load = useCallback(async () => {
    void fetchSiteLogins().then(setSites, () => {});
    try {
      const rows = await fetchHosterLogins();
      setLogins(rows);
      onEnabledHosts?.(
        rows
          .filter((r) => r.enabled)
          .map((r) => r.host)
          .sort()
          .join(','),
      );
    } catch {
      // A missed poll keeps the previous rows.
    }
  }, [onEnabledHosts]);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [load]);

  useEffect(() => {
    void fetchHosterHosts()
      .then(setHosts)
      .catch(() => {});
  }, []);

  // Optimistic; the reload corrects the rows once JD has reconciled.
  async function switchHosts(hosts: string[], enabled: boolean) {
    const changing = new Set(hosts);
    setLogins((cur) => cur?.map((x) => (changing.has(x.host) ? { ...x, enabled } : x)) ?? cur);
    try {
      await Promise.all(hosts.map((host) => setHosterLoginEnabled(host, enabled)));
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
    await load();
  }

  const toggle = (row: HosterLogin, enabled: boolean) => switchHosts([row.host], enabled);
  const toggleAll = (enabled: boolean) =>
    switchHosts((logins ?? []).filter((l) => l.enabled !== enabled).map((l) => l.host), enabled);

  async function remove(host: string) {
    try {
      await removeHosterLogin(host);
      toast(t('accounts.hoster.removed'), 'info');
      await load();
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
  }

  const putSite = useCallback((site: SiteLogin) => setSites((cur) => cur.map((x) => (x.id === site.id ? site : x))), []);

  async function addSite(site: SiteLogin) {
    try {
      const added = await addSiteLogin(site.id);
      putSite(added);
      return added;
    } catch {
      toast(t('common.loadFailed'), 'fail');
      return undefined;
    }
  }

  async function removeSite(site: SiteLogin) {
    try {
      putSite(await removeSiteLogin(site.id));
      toast(t('accounts.hoster.removed'), 'info');
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
  }

  const ways = { readsBrowsers: readsBrowsers(sites) };
  return { logins, sites, ways, hosts, load, toggle, toggleAll, remove, putSite, addSite, removeSite };
}

/**
 * signInPageOf is a hoster login's own sign-in page as the sign-in browser
 * opens it. Only the window's title and logo read more than the id.
 */
function signInPageOf(login: HosterLogin): SiteLogin {
  return {
    id: login.signInSite ?? '',
    name: login.host,
    host: login.host,
    loginUrl: '',
    domains: [],
    state: login.status === 'active' ? 'signedIn' : 'signedOut',
    unreadable: [],
    readsBrowsers: false,
    listed: true,
  };
}

/** HosterLoginSection is the hoster card. */
export function HosterLoginSection({
  data,
  adding,
  onAddClose,
}: {
  data: HosterLogins;
  /** The page asked for the window that adds a login. */
  adding: boolean;
  onAddClose: () => void;
}) {
  const { t, lang } = useT();
  const [editing, setEditing] = useState<HosterLogin | null>(null);
  const [editingSite, setEditingSite] = useState<SiteLogin | null>(null);
  // The login or site awaiting removal confirmation.
  const [confirming, setConfirming] = useState<HosterLogin | null>(null);
  const [confirmingSite, setConfirmingSite] = useState<SiteLogin | null>(null);
  const [browsingSite, setBrowsingSite] = useState<SiteLogin | null>(null);
  const [browsingLogin, setBrowsingLogin] = useState<HosterLogin | null>(null);
  const browserHere = useSignInBrowserAvailable();
  // Where KnightLoader reads no browser, the sign-in window is the only way in,
  // so a site nobody signed in to opens it at once instead of a window that points to it.
  const openSite = (site: SiteLogin) => {
    if (!data.ways.readsBrowsers && !signedIn(site)) setBrowsingSite(site);
    else setEditingSite(site);
  };

  const rows = data.logins ?? [];
  const added = data.sites.filter((s) => s.listed);
  const hasRows = rows.length > 0 || added.length > 0;
  const unreadable = unreadableBrowsers(added);

  return (
    <div className="flex flex-col gap-3">
      {hasRows && (
        <ConnectionList label={t('accounts.hoster.title')} data-new="site-logins">
          {[...rows].sort(byHost).map((row) => (
            <HosterConnection
              key={row.host}
              login={row}
              onToggle={(v) => void data.toggle(row, v)}
              onEdit={() => setEditing(row)}
              onRemove={() => setConfirming(row)}
              onSignInHere={browserHere && row.signInSite ? () => setBrowsingLogin(row) : undefined}
            />
          ))}
          {[...added].sort(byName).map((site) => (
            <SiteConnection
              key={site.id}
              site={site}
              ways={data.ways}
              onChanged={data.putSite}
              onEdit={() => openSite(site)}
              onRemove={() => setConfirmingSite(site)}
              onSignInHere={browserHere ? () => setBrowsingSite(site) : undefined}
            />
          ))}
        </ConnectionList>
      )}

      {unreadable.length > 0 && (
        <p className="text-xs text-carbon-textMuted">
          {t('siteLogins.unreadable', { browsers: new Intl.ListFormat(lang, { type: 'conjunction' }).format(unreadable) })}
        </p>
      )}

      {!hasRows && (
        <EmptyState
          nested
          icon={<IconAccounts width={26} height={26} />}
          title={t('accounts.hoster.empty')}
        />
      )}

      {(editing || adding) && (
        <HosterLoginDialog
          hosts={data.hosts}
          sites={data.sites}
          existing={rows}
          editing={editing ?? undefined}
          onClose={() => {
            setEditing(null);
            onAddClose();
          }}
          onSaved={data.load}
          onPickSite={(site) => {
            onAddClose();
            void data.addSite(site).then((next) => next && openSite(next));
          }}
        />
      )}

      {editingSite && (
        <SiteLoginDialog
          site={editingSite}
          ways={data.ways}
          hue={1}
          onChanged={data.putSite}
          onClose={() => setEditingSite(null)}
        />
      )}

      {browsingSite && (
        <SignInBrowserWindow site={browsingSite} hue={1} onChanged={data.putSite} onClose={() => setBrowsingSite(null)} />
      )}

      {browsingLogin && (
        <SignInBrowserWindow
          site={signInPageOf(browsingLogin)}
          hue={1}
          onChanged={() => void data.load()}
          onClose={() => setBrowsingLogin(null)}
        />
      )}

      {confirming && (
        <ConfirmRemoveLogin
          name={confirming.host}
          onCancel={() => setConfirming(null)}
          onConfirm={() => {
            setConfirming(null);
            void data.remove(confirming.host);
          }}
        />
      )}

      {confirmingSite && (
        <ConfirmRemoveLogin
          name={confirmingSite.name}
          onCancel={() => setConfirmingSite(null)}
          onConfirm={() => {
            setConfirmingSite(null);
            void data.removeSite(confirmingSite);
          }}
        />
      )}
    </div>
  );
}

function byHost(a: HosterLogin, b: HosterLogin): number {
  return a.host.localeCompare(b.host, pageLang(), { sensitivity: 'base' });
}

function byName(a: SiteLogin, b: SiteLogin): number {
  return a.name.localeCompare(b.name, pageLang(), { sensitivity: 'base' });
}

/** ConfirmRemoveLogin asks first, since a stored password or session cannot be read back. */
function ConfirmRemoveLogin({
  name,
  onCancel,
  onConfirm,
}: {
  name: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT();
  return (
    <Modal
      title={t('accounts.remove')}
      hue={1}
      onClose={onCancel}
      footer={
        <>
          {/* Matches the debrid card's confirmation footer. */}
          <span className="flex-1" />
          <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onCancel} />
          <Button kind="secondary" icon={<IconTrash width={16} height={16} />} onClick={onConfirm}>
            {t('accounts.remove')}
          </Button>
        </>
      }
    >
      <p className="text-sm text-carbon-text">{t('accounts.removeConfirm', { name })}</p>
    </Modal>
  );
}

/**
 * loginDetail words why a login is queued or rejected, from its code where
 * this build knows it and in the server's sentence otherwise.
 */
function loginDetail(t: (key: TranslationKey) => string, login: HosterLogin): string | undefined {
  const key = `accounts.hoster.detail.${login.code ?? ''}` as TranslationKey;
  return login.code && key in en ? t(key) : login.detail;
}

/**
 * SiteConnection is a site signed into through the browser as a connection
 * row: who its session signs in as and where the session came from, or, without
 * a working one, the one step that signs it in. Test asks the site whether the
 * session still holds.
 */
function SiteConnection({
  site,
  ways,
  onChanged,
  onEdit,
  onRemove,
  onSignInHere,
}: {
  site: SiteLogin;
  ways: SignInWays;
  onChanged: (site: SiteLogin) => void;
  onEdit: () => void;
  onRemove: () => void;
  /** Opens the sign-in browser on the site, to refresh a session or switch accounts. */
  onSignInHere?: () => void;
}) {
  const { t } = useT();
  const [test, setTest] = useState<ConnectionTest>();
  const note = stateNote(site);
  const from = sourceNote(site);
  const step = nextStep(site, ways);
  const working = signedIn(site);

  async function runTest() {
    setTest({ busy: true });
    try {
      const next = await checkSiteLogin(site.id);
      onChanged(next);
      const found = stateNote(next);
      // A site that did not answer leaves the session in use, which is neither verdict.
      const tone = next.state === 'signedIn' ? 'ok' : next.state === 'unchecked' ? undefined : 'danger';
      setTest({ tone, title: t(found.key, found.vars) });
    } catch {
      setTest({ tone: 'danger', title: t('common.loadFailed') });
    }
  }

  return (
    <ServiceConnection
      name={site.name}
      host={site.host}
      status={working ? t(note.key, note.vars) : step && t(step.key, step.vars)}
      badges={!working && <LabelBadge tone="fail" label={t(note.key, note.vars)} />}
      test={test}
      text={{ test: t('siteLogins.test'), edit: t('siteLogins.edit'), remove: t('accounts.remove') }}
      actions={
        onSignInHere && [{ id: 'sign-in-here', label: t('signInBrowser.signInHere'), icon: <IconSignIn />, onClick: onSignInHere }]
      }
      onTest={site.state === 'signedOut' ? undefined : () => void runTest()}
      onEdit={onEdit}
      onRemove={onRemove}
    >
      {from && <span>{t(from.key, from.vars)}</span>}
      {working && site.expires && <span className="glim-num">{t('siteLogins.runsUntil', { date: fmtDate(site.expires) })}</span>}
    </ServiceConnection>
  );
}

/**
 * HosterConnection is one login as a connection row: its plan, how long it
 * runs and what is left of its traffic once JD has signed in with it, or why
 * not. A login waiting for a code or a sign-in has its own word, since that
 * names the person's next step.
 */
function HosterConnection({
  login,
  onToggle,
  onEdit,
  onRemove,
  onSignInHere,
}: {
  login: HosterLogin;
  onToggle: (enabled: boolean) => void;
  onEdit: () => void;
  onRemove: () => void;
  /** Opens the sign-in browser on the hoster's own sign-in page. */
  onSignInHere?: () => void;
}) {
  const { t } = useT();
  const off = !login.enabled || login.status === 'off';
  const refused = login.signInHere
    ? 'accounts.hoster.status.needsSignIn'
    : login.askCode
      ? 'accounts.hoster.status.needsCode'
      : 'accounts.hoster.status.rejected';
  const badge = off ? (
    <LabelBadge label={t('accounts.disabled')} />
  ) : login.status === 'rejected' ? (
    <span data-new="login-waits" className="inline-flex">
      <LabelBadge tone="fail" label={t(refused)} />
    </span>
  ) : (
    // 'queued' covers both "not yet confirmed" and "not yet validated" by
    // JD; the status line says which.
    login.status === 'queued' && <LabelBadge label={t('accounts.hoster.status.queued')} />
  );
  const plan = planLine(t, login.tier, login.expiry);
  const status = login.status === 'active' ? (plan ?? t('accounts.hoster.status.active')) : off ? plan : loginDetail(t, login);
  return (
    <ServiceConnection
      name={login.host}
      catalogMark={catalogEntry(DOWNLOAD_PROVIDERS, login.host)?.mark}
      host={login.host}
      status={status}
      badges={badge || undefined}
      text={{ test: t('siteLogins.test'), edit: t('siteLogins.edit'), remove: t('accounts.remove') }}
      actions={[
        ...(onSignInHere ? [{ id: 'sign-in-here', label: t('signInBrowser.signInHere'), icon: <IconSignIn />, onClick: onSignInHere }] : []),
        switchAction(t, login.enabled, onToggle),
      ]}
      onEdit={onEdit}
      onRemove={onRemove}
    >
      {/* JD reports bytes left and max; without a max there is no allowance to show. */}
      {!!login.trafficMax && (
        <TrafficLine traffic={{ used: Math.max(0, login.trafficMax - (login.trafficLeft ?? 0)), limit: login.trafficMax }} />
      )}
      {/* Why JD has a login KnightLoader signs in with itself. */}
      {login.handedOver && <span>{t('accounts.hoster.handedOver', { host: login.host })}</span>}
    </ServiceConnection>
  );
}

// accounts.Redacted: sent back unchanged, it keeps the stored password.
const REDACTED = '********';

// The six digits an authenticator app shows.
const CODE = /^\d{6}$/;

// Why the code field is there, by the row's code. Rapidgator answers a
// two-factor login without its code with either of the last two at times,
// so the code is offered there as well.
const CODE_NOTES: Record<string, TranslationKey> = {
  ownCodeNeeded: 'accounts.hoster.codeNeeded',
  ownCodeWrong: 'accounts.hoster.codeWrong',
  ownCodeEnded: 'accounts.hoster.codeEnded',
  ownActivate: 'accounts.hoster.codeActivate',
  ownNewAddress: 'accounts.hoster.codeNewAddress',
};

// The same refusals once a code did not get past them either.
const CODE_REFUSED: Record<string, TranslationKey> = {
  ownActivate: 'accounts.hoster.codeDidNotHelp',
  ownNewAddress: 'accounts.hoster.codeDidNotHelpAddress',
};

/** offersCode reports whether a code could get the login in. */
const offersCode = (login?: HosterLogin | null) => !!login && (!!login.askCode || !!login.codeMayHelp);

/**
 * HosterLoginDialog stores one login and stays open to test it: it says
 * "Connected" once the hoster takes the login, or why not, and offers the code
 * of a second factor where one could help.
 */
function HosterLoginDialog({
  hosts,
  sites,
  existing,
  editing,
  onClose,
  onSaved,
  onPickSite,
}: {
  hosts: HosterHost[];
  sites: SiteLogin[];
  existing: HosterLogin[];
  editing?: HosterLogin;
  onClose: () => void;
  onSaved: () => Promise<void>;
  /** A site picked from the list, which signs in through the browser rather than this form. */
  onPickSite: (site: SiteLogin) => void;
}) {
  const { t } = useT();
  const [query, setQuery] = useState('');
  const [last, setLast] = useState<string>();
  const [picked, setPicked] = useState<HosterHost | null>(
    editing ? { id: editing.host, label: editing.host } : null,
  );
  const [username, setUsername] = useState(editing?.username ?? '');
  const [password, setPassword] = useState(editing ? REDACTED : '');
  const [saving, setSaving] = useState(false);
  // The row the last save came back with, followed while JD checks it. A
  // login opened while a code could get it in starts out on its own row.
  const [tested, setTested] = useState<HosterLogin | null>(offersCode(editing) ? editing! : null);
  // The fields as last saved, so a connected login offers Save again once
  // either changes.
  const [savedAs, setSavedAs] = useState<{ username: string; password: string } | null>(null);
  // The code is sent once and kept nowhere. withCode is whether the last
  // save sent one.
  const [code, setCode] = useState('');
  const [withCode, setWithCode] = useState(false);
  // Why the last save did not reach the hoster, said in the window itself.
  const [failure, setFailure] = useState('');
  const mounted = useRef(true);
  useEffect(
    () => () => {
      mounted.current = false;
    },
    [],
  );

  const waitingOn = tested?.status === 'queued' ? tested.host : null;
  useEffect(() => {
    if (!waitingOn) return;
    const timer = window.setInterval(() => {
      void fetchHosterLogins()
        .then((rows) => {
          const now = rows.find((r) => r.host === waitingOn);
          if (now && now.status !== 'queued' && mounted.current) setTested(now);
        })
        .catch(() => {});
    }, TEST_POLL_MS);
    return () => window.clearInterval(timer);
  }, [waitingOn]);

  // A site signed into through the browser is offered until it is added, and
  // stands in for JD's entry of the same host, whose password login the site
  // does not take.
  // GlimStone's catalog names the hosters it knows and gives their marks, by
  // the domain a login is filed under; the rest bring the logo their site has.
  const configured = new Set(existing.map((e) => e.host));
  const offered = hosts.filter((h) => !configured.has(h.id) && !siteForHost(h.id, sites));
  const known = new Set<string>(DOWNLOAD_PROVIDERS.entries.filter((e) => e.group === 'hoster').map((e) => e.id));
  const brands: Brand[] = [
    ...catalogBrands(
      DOWNLOAD_PROVIDERS,
      {
        ids: offered.filter((h) => known.has(h.id)).map((h) => h.id as DownloadProviderId),
        titles: { debrid: '', hoster: '' },
        glyph: () => <IconCloud />,
        lang: pageLang(),
      },
      (id) => ({ sub: id }),
      (id, mark) => ownTile('download', id, mark, id),
    ).flatMap((g) => g.brands),
    ...offered.filter((h) => !known.has(h.id)).map((h) => ({ id: h.id, name: h.label, host: h.id, sub: h.id })),
    ...sites.filter((s) => !s.listed).map((s) => ({ id: SITE_BRAND + s.id, name: s.name, host: s.host, sub: s.host })),
  ].sort((a, b) => a.name.localeCompare(b.name, pageLang(), { sensitivity: 'base' }));

  const pick = (id: string) => {
    const site = id.startsWith(SITE_BRAND) ? sites.find((s) => SITE_BRAND + s.id === id) : undefined;
    if (site) onPickSite(site);
    else {
      setLast(id);
      setPicked(hosts.find((h) => h.id === id) ?? null);
    }
  };

  async function doSave() {
    if (!picked) return;
    setSaving(true);
    setFailure('');
    try {
      const row = await saveHosterLogin(picked.id, username, password, code || undefined);
      if (!mounted.current) return;
      setTested(row);
      setWithCode(code !== '');
      setCode('');
      setSavedAs({ username, password });
      await onSaved();
    } catch (e) {
      // The code stays, since the hoster never saw it.
      if (mounted.current) setFailure(e instanceof Error ? e.message : String(e));
    } finally {
      if (mounted.current) setSaving(false);
    }
  }

  // The same titles as the debrid card's window, with this card's own noun.
  const title = !picked
    ? t('accounts.hoster.pickAccount')
    : editing
      ? t('accounts.editCredentialTitle', { service: picked.label })
      : t('accounts.addAccountTitle', { service: picked.label });
  const asksCode = offersCode(tested);
  // A refusal a code only may get past can be tested again without one.
  const codeOk = tested?.askCode ? CODE.test(code) : code === '' || CODE.test(code);
  const canSave = username.trim() !== '' && password.trim() !== '' && (!asksCode || codeOk);
  const connected = tested?.status === 'active' && savedAs?.username === username && savedAs.password === password;
  // Only a code the hoster turned down, or one that did not get past its
  // refusal, reads as an error.
  const refused = withCode ? CODE_REFUSED[tested?.code ?? ''] : undefined;
  const codeWrong = tested?.code === 'ownCodeWrong' || refused !== undefined;
  const codeNote = t(refused ?? CODE_NOTES[tested?.code ?? ''] ?? 'accounts.hoster.codeNeeded');
  // Beside the code field its own note says why the hoster refused.
  const outcome =
    saving || tested?.status === 'queued' ? (
      <TestNote tone="neutral">{t('accounts.hoster.testing')}</TestNote>
    ) : failure ? (
      <TestNote tone="fail">{failure}</TestNote>
    ) : !tested || asksCode ? null : tested.status === 'active' ? (
      <TestNote tone="ok">{t('accounts.hoster.connected')}</TestNote>
    ) : (
      <TestNote tone={tested.status === 'rejected' ? 'fail' : 'neutral'}>{loginDetail(t, tested)}</TestNote>
    );

  return (
    <Modal
      title={title}
      // While adding, the window says what the card it fills is for.
      hint={editing ? undefined : t('accounts.hoster.hint')}
      hue={1}
      // The picker's window, which the form of the picked hoster stays in, and
      // an edit opens the same form in the same window.
      wide
      height="capped"
      holdHeight={!picked && query !== ''}
      closeOnBackdrop={!picked}
      right={!picked && <PickerSearch value={query} onChange={setQuery} placeholder={t('accounts.hoster.searchHosts')} />}
      onClose={onClose}
      footer={
        picked ? (
          <>
            {/* A new login's form leads back to the list it was picked from;
                an edit stays on its host and offers the way out. */}
            {connected ? (
              <Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={onClose} />
            ) : editing ? (
              <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
            ) : (
              <Button
                kind="secondary"
                labelled
                icon={<IconBack className="rtl:-scale-x-100" />}
                title={t('onboarding.back')}
                onClick={() => {
                  // The list comes back whole, so the window can measure the height its search holds.
                  setPicked(null);
                  setQuery('');
                }}
              />
            )}
            <span className="flex-1" />
            {connected ? (
              <Button icon={<IconCheck />} disabled>
                {t('accounts.hoster.connected')}
              </Button>
            ) : (
              <Button icon={<IconSave />} onClick={() => void doSave()} disabled={saving || !canSave}>
                {saving ? t('accounts.saving') : t('accounts.save')}
              </Button>
            )}
          </>
        ) : (
          <>
            <span className="flex-1" />
            <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          </>
        )
      }
    >
      {!picked ? (
        <div data-new="hoster-logos">
          <BrandPicker
            brands={brands.filter((b) => brandMatches(b, query))}
            none={t('accounts.noServicesFound')}
            selected={last}
            onPick={pick}
          />
        </div>
      ) : (
        <div data-new="picker-form" className="flex flex-col gap-4">
          <SettingRow control="long" label={t('accounts.usernameField')}>
            <TextInput autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
          </SettingRow>
          <SettingRow control="long" label={t('accounts.passwordField')} hint={t('accounts.hoster.passwordCustody')}>
            <TextInput type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </SettingRow>
          {outcome && <div data-new="login-test">{outcome}</div>}
          {asksCode && (
            <div data-new="hoster-code" className="flex flex-col gap-1.5">
              <p
                id="hoster-code-note"
                role={codeWrong ? 'alert' : undefined}
                className={`text-meta ${codeWrong ? 'text-statusFail' : 'text-carbon-textMuted'}`}
              >
                {codeNote}
              </p>
              <SettingRow control="long" label={t('accounts.hoster.codeField')} hint={t('accounts.hoster.codeHint')}>
                <TextInput
                  autoFocus
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  aria-invalid={codeWrong && code === ''}
                  aria-describedby="hoster-code-note"
                  className="tabular-nums tracking-[0.2em]"
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && canSave && !saving) void doSave();
                  }}
                />
              </SettingRow>
            </div>
          )}
          {editing?.signInSite && (
            <div className="flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
              <p className="min-w-0 flex-[1_1_12.5rem] text-sm text-carbon-textMuted">{t('accounts.hoster.signInHereHint')}</p>
              <SignInHereButton
                site={signInPageOf(editing)}
                hue={1}
                disabled={saving}
                onChanged={(s) => {
                  if (s.state === 'signedIn') void onSaved();
                }}
              />
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}

const TONES = {
  ok: ['text-statusOk', 'bg-statusOkSolid'],
  fail: ['text-statusFail', 'bg-statusFailSolid'],
  neutral: ['text-statusNeutral', 'bg-statusNeutralSolid'],
} as const;

/** TestNote is what testing a login came to, in the colours of the row's states. */
function TestNote({ tone, children }: { tone: keyof typeof TONES; children: ReactNode }) {
  const [text, dot] = TONES[tone];
  return (
    <p role="status" className={`flex items-start gap-2 text-sm font-medium ${text}`}>
      <span className={`mt-[0.5em] h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] ${dot}`} />
      <span>{children}</span>
    </p>
  );
}
