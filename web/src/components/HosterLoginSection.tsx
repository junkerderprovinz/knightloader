// Hoster logins for the headless JD sidecar (see internal/hosterauth). Saving
// writes the credential into JD's account config through its Remote API, and
// JD's hoster plugin performs the login; JD's own UI is never shown. The table
// is AccountTable, shared with the debrid card; this file adds the JD status
// badge and the host picker. The picker offers no debrid service: KnightLoader
// speaks to each of those itself. The card's Add button on the accounts page
// opens the picker through `adding`.
//
// The sites signed into through the browser (internal/sitelogin) are rows
// here too, always, since KnightLoader picks a sign-in up by itself: each says
// who it signs in as and where from, or how to sign in, and opens
// SiteLoginDialog rather than a password form.
import { useCallback, useEffect, useState } from 'react';
import {
  type HosterHost,
  type HosterLogin,
  type SiteLogin,
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
import { type SignInWays, nextStep, siteForHost, sourceNote, stateNote, unreadableBrowsers } from '../lib/siteLogins';
import { useToast } from '../lib/toast';
import { Button, EmptyState, InfoBubble, Modal, TextInput } from './ui';
import { AccountTable, type AccountRow } from './AccountTable';
import { IconAccounts, IconBack, IconClose, IconSave, IconTrash } from '../lib/icons';
import { BrandPicker, PickerSearch, brandMatches, type Brand } from './BrandPicker';
import { SiteLoginDialog, useSignInWays } from './SiteLoginDialog';
import { SignInBrowserWindow, useSignInBrowserAvailable } from './SignInBrowserWindow';
import { SettingRow } from '../pages/settings/controls';

// Faster than the 30s account health poll, since a new login moves from queued
// to active or rejected within seconds to minutes.
const POLL_MS = 8000;

/** HosterLogins is the logins, the sites signed into through the browser and the host list. */
export interface HosterLogins {
  logins: HosterLogin[] | null;
  sites: SiteLogin[];
  /** Where this instance can get a sign-in from, which the sites' rows tell. */
  ways: SignInWays;
  hosts: HosterHost[];
  load: () => Promise<void>;
  toggle: (row: HosterLogin, enabled: boolean) => Promise<void>;
  remove: (host: string) => Promise<void>;
  putSite: (site: SiteLogin) => void;
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

  async function toggle(row: HosterLogin, enabled: boolean) {
    // Optimistic; the reload corrects the row once JD has reconciled.
    setLogins((cur) => cur?.map((x) => (x.host === row.host ? { ...x, enabled } : x)) ?? cur);
    try {
      await setHosterLoginEnabled(row.host, enabled);
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
    await load();
  }

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

  async function removeSite(site: SiteLogin) {
    try {
      putSite(await removeSiteLogin(site.id));
      toast(t('siteLogins.signedOutToast', { site: site.name }), 'info');
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
  }

  const ways = useSignInWays(sites);
  return { logins, sites, ways, hosts, load, toggle, remove, putSite, removeSite };
}

/**
 * siteLoginRow is one site signed into through the browser as an AccountTable
 * row. Removing it signs KnightLoader out whatever the session came from, and
 * leaves the browser signed in. here opens the sign-in browser on the site.
 */
function siteLoginRow(
  site: SiteLogin,
  ways: SignInWays,
  actions: { onEdit: () => void; onRemove: () => void },
  here?: { label: string; onSelect: () => void },
): AccountRow {
  return {
    key: `site:${site.id}`,
    iconHost: site.host,
    label: site.name,
    enabled: true,
    status: <SiteLoginStatus site={site} ways={ways} />,
    expiry: site.expires,
    onEdit: actions.onEdit,
    onRemove: site.state !== 'signedOut' ? actions.onRemove : undefined,
    menu: here ? [{ id: 'actions', items: [{ id: 'signin-here', ...here }] }] : undefined,
  };
}

/** hosterLoginRow is one login as an AccountTable row. */
function hosterLoginRow(
  row: HosterLogin,
  actions: { onToggle: (v: boolean) => void; onEdit: () => void; onRemove: () => void },
): AccountRow {
  return {
    key: row.host,
    iconHost: row.host,
    label: row.host,
    enabled: row.enabled,
    status: <HosterLoginStatusBadge login={row} />,
    tier: row.tier,
    expiry: row.expiry,
    // JD reports bytes left and max; without a max the row shows a dash.
    traffic: { used: Math.max(0, (row.trafficMax ?? 0) - (row.trafficLeft ?? 0)), limit: row.trafficMax ?? 0 },
    ...actions,
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
  const browserHere = useSignInBrowserAvailable();

  const rows = data.logins ?? [];
  const hasRows = rows.length > 0 || data.sites.length > 0;
  const unreadable = unreadableBrowsers(data.sites);

  return (
    <div className="flex flex-col gap-3">
      {hasRows && (
        <AccountTable
          label={t('accounts.hoster.title')}
          rows={[
            ...rows.map((row) =>
              hosterLoginRow(row, {
                onToggle: (v) => void data.toggle(row, v),
                onEdit: () => setEditing(row),
                onRemove: () => setConfirming(row),
              }),
            ),
            ...data.sites.map((site) =>
              siteLoginRow(
                site,
                data.ways,
                { onEdit: () => setEditingSite(site), onRemove: () => setConfirmingSite(site) },
                browserHere ? { label: t('signInBrowser.signInHere'), onSelect: () => setBrowsingSite(site) } : undefined,
              ),
            ),
          ]}
        />
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
        />
      )}

      {editingSite && (
        <SiteLoginDialog
          site={editingSite}
          ways={data.ways}
          hue={2}
          onChanged={data.putSite}
          onClose={() => setEditingSite(null)}
        />
      )}

      {browsingSite && (
        <SignInBrowserWindow site={browsingSite} hue={2} onChanged={data.putSite} onClose={() => setBrowsingSite(null)} />
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
      hue={2}
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
 * SiteLoginStatus says who a site's session signs in as and where it came
 * from, or, without a working session, the one step that signs it in.
 */
function SiteLoginStatus({ site, ways }: { site: SiteLogin; ways: SignInWays }) {
  const { t } = useT();
  const note = stateNote(site);
  const details = [sourceNote(site), nextStep(site, ways)].filter((n) => n !== undefined);
  const [text, dot] = note.warn
    ? ['text-statusWarn', 'bg-statusWarnSolid']
    : site.state === 'signedOut'
      ? ['text-carbon-textMuted', 'bg-carbon-textMuted']
      : ['text-statusOk', 'bg-statusOkSolid'];
  return (
    <span className="flex max-w-[24rem] flex-col gap-0.5">
      <span className={`inline-flex items-center gap-1.5 text-meta font-medium ${text}`}>
        <span className={`h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] ${dot}`} />
        {t(note.key, note.vars)}
      </span>
      {details.map((d) => (
        <span key={d.key} className="text-meta text-carbon-textMuted">
          {t(d.key, d.vars)}
        </span>
      ))}
    </span>
  );
}

function HosterLoginStatusBadge({ login }: { login: HosterLogin }) {
  const { t } = useT();
  const detail = loginDetail(t, login);
  // Beside JD's word on the login, why JD has a login KnightLoader signs in
  // with itself.
  const handed = login.handedOver && <InfoBubble tip={t('accounts.hoster.handedOver', { host: login.host })} />;
  switch (login.status) {
    case 'off':
      // Not "queued": JD does not have a switched-off login at all.
      return (
        <span className="inline-flex items-center gap-1.5 text-meta font-medium text-carbon-textMuted">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-carbon-textMuted" />
          {t('accounts.hoster.status.off')}
        </span>
      );
    case 'active':
      return (
        <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusOk">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusOkSolid" />
          {t('accounts.hoster.status.active')}
          {handed}
        </span>
      );
    case 'rejected':
      // Waiting for a code is the person's next step, not a dead login.
      if (login.askCode) {
        return (
          <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusWarn">
            <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusWarnSolid" />
            {t('accounts.hoster.status.needsCode')}
            {detail && <InfoBubble tip={detail} />}
          </span>
        );
      }
      return (
        <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusFail">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusFailSolid" />
          {t('accounts.hoster.status.rejected')}
          {detail && <InfoBubble tip={detail} />}
          {handed}
        </span>
      );
    default:
      // 'queued' covers both "not yet confirmed" and "not yet validated" by JD;
      // the detail text says which.
      return (
        <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusNeutral">
          <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusNeutralSolid" />
          {t('accounts.hoster.status.queued')}
          {detail && <InfoBubble tip={detail} />}
          {handed}
        </span>
      );
  }
}

// accounts.Redacted: sent back unchanged, it keeps the stored password.
const REDACTED = '********';

// The six digits an authenticator app shows.
const CODE = /^\d{6}$/;

/** HosterLoginDialog stores one login. */
function HosterLoginDialog({
  hosts,
  sites,
  existing,
  editing,
  onClose,
  onSaved,
}: {
  hosts: HosterHost[];
  sites: SiteLogin[];
  existing: HosterLogin[];
  editing?: HosterLogin;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [picked, setPicked] = useState<HosterHost | null>(
    editing ? { id: editing.host, label: editing.host } : null,
  );
  const [username, setUsername] = useState(editing?.username ?? '');
  const [password, setPassword] = useState(editing ? REDACTED : '');
  const [saving, setSaving] = useState(false);
  // The row of a login the hoster takes only with a second factor's code,
  // whose field the form then shows. The code is sent once and kept nowhere.
  const [asked, setAsked] = useState<HosterLogin | null>(editing?.askCode ? editing : null);
  const [code, setCode] = useState('');

  // A site signed into through the browser has its own row, and stands in for
  // JD's entry of the same host, whose password login the site does not take.
  const configured = new Set(existing.map((e) => e.host));
  const brands: Brand[] = hosts
    .filter((h) => !configured.has(h.id) && !siteForHost(h.id, sites))
    .map((h) => ({ id: h.id, name: h.label, host: h.id }))
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));

  const pick = (id: string) => setPicked(hosts.find((h) => h.id === id) ?? null);

  async function doSave() {
    if (!picked) return;
    setSaving(true);
    try {
      const row = await saveHosterLogin(picked.id, username, password, asked ? code : undefined);
      await onSaved();
      if (row.askCode) {
        setAsked(row);
        setCode('');
        return;
      }
      toast(t('accounts.saved'), 'ok');
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    } finally {
      setSaving(false);
    }
  }

  // The same titles as the debrid card's window, with this card's own noun.
  const title = !picked
    ? t('accounts.hoster.pickAccount')
    : editing
      ? t('accounts.editCredentialTitle', { service: picked.label })
      : t('accounts.addAccountTitle', { service: picked.label });
  const canSave = username.trim() !== '' && password.trim() !== '' && (!asked || CODE.test(code));
  // Why the field is there. Only a code the hoster turned down is the
  // person's to correct, so only that one reads as an error.
  const codeWrong = asked?.code === 'ownCodeWrong';
  const codeNote = codeWrong
    ? t('accounts.hoster.codeWrong')
    : asked?.code === 'ownCodeEnded'
      ? t('accounts.hoster.codeEnded')
      : t('accounts.hoster.codeNeeded');

  return (
    <Modal
      title={title}
      // While adding, the window says what the card it fills is for.
      hint={editing ? undefined : t('accounts.hoster.hint')}
      hue={2}
      // The picker's window, which the form of the picked hoster stays in, and
      // an edit opens the same form in the same window.
      wide
      height={picked ? 'content' : 'capped'}
      holdHeight={!picked && query !== ''}
      right={!picked && <PickerSearch value={query} onChange={setQuery} placeholder={t('accounts.hoster.searchHosts')} />}
      onClose={onClose}
      footer={
        picked ? (
          <>
            {/* A new login's form leads back to the list it was picked from;
                an edit stays on its host and offers the way out. */}
            {editing ? (
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
            <Button icon={<IconSave />} onClick={() => void doSave()} disabled={saving || !canSave}>
              {saving ? t('accounts.saving') : t('accounts.save')}
            </Button>
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
          {asked && (
            <div className="flex flex-col gap-1.5">
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
              <p
                id="hoster-code-note"
                role={codeWrong ? 'alert' : undefined}
                className={`text-meta ${codeWrong ? 'text-statusFail' : 'text-carbon-textMuted'}`}
              >
                {codeNote}
              </p>
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}
