// The accounts page: one row per configured service and account, as read from
// internal/accounts/catalogue.go and internal/app/app_accounts.go. Debrid
// accounts come first, the own Usenet servers below them, then hoster logins,
// the logins for the user's own servers and the captcha solvers' keys; the
// section follows the catalogue's Group field, and every card draws an
// AccountTable. Under them comes the Allow free downloads switch, since whether
// a link may be fetched for free is a question of which accounts there are, and
// last the two priority cards side by side, one for the download backends and
// one for the captcha accounts.
// Adding is the page's one floating action, which opens a window with a tile
// per kind in the cards' order; no card carries an add button of its own.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  type Account,
  type AccountCredential,
  type CatalogueService,
  type CredentialField,
  type ResolverInfo,
  type VerifyResult,
  fetchAccounts,
  fetchAccountCatalogue,
  fetchResolverPriority,
  removeAccountCredential,
  saveAccountCredential,
  saveResolverPriority,
  setAccountEnabled,
  testAccount,
  verifyAccountCredential,
} from '../lib/api';
import { openExternal } from '../lib/external';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { fmtDate } from '../lib/format';
import { resolverLabel } from '../lib/resolverLabels';
import {
  Button,
  Card,
  EmptyState,
  ErrorCard,
  Field,
  InfoBubble,
  LinkBadge,
  LoadingCard,
  Modal,
  PageHeader,
  PasswordInput,
  SectionTitle,
  TextInput,
  Toggle,
} from '../components/ui';
import { AccountTable, type AccountRow } from '../components/AccountTable';
import { FreeDownloadsCard } from '../components/FreeDownloadsCard';
import { HosterLoginSection, useHosterLogins } from '../components/HosterLoginSection';
import { PageAction, PageActions } from '../components/PageActions';
import { UsenetServersSection } from '../components/UsenetServersSection';
import { CaptchaPriorityCard } from './settings/accounts/CaptchaPriorityCard';
import { LiveSettings } from './settings/accounts/LiveSettings';
import {
  IconAccounts,
  IconCaptcha,
  IconBack,
  IconClose,
  IconExternalLink,
  IconGlobe,
  IconFleet,
  IconKey,
  IconAdd,
  IconRefresh,
  IconTrash,
} from '../lib/icons';
import {
  BrandPicker,
  KindPicker,
  PickerSearch,
  brandMatches,
  siteOf,
  type Brand,
  type Kind,
} from '../components/BrandPicker';
import { PriorityCard, PriorityLadder } from '../components/PriorityLadder';

// Reads what the background account-health refresher stored; expiry and
// traffic change in hours.
const HEALTH_POLL_MS = 30000;

/** The form's caption for each named credential field. */
const FIELD_LABELS: Record<CredentialField, TranslationKey> = {
  apiUser: 'accounts.field.apiUser',
  apiKey: 'accounts.field.apiKey',
  customerId: 'accounts.field.customerId',
  email: 'accounts.field.email',
};

/** The catalogue groups this page adds and edits accounts of itself. */
type AccountGroup = 'debrid' | 'captchaSolver';

type DialogState =
  | { mode: 'new'; group: AccountGroup }
  | { mode: 'edit'; service: string; account: string }
  // An own server's login, keyed by hostname; host is null for a new one.
  | { mode: 'server'; service: CatalogueService; host: string | null };

/**
 * serverHost reduces what was typed into the hostname field to the bare host,
 * lower-cased and without port or path, since remotefs looks a link's login up
 * by url.Hostname. A pasted link is cut down the same way. Anything else comes
 * back empty: Chromium lets a space or a "!" through and percent-encodes it,
 * and no link would ever carry that name.
 */
function serverHost(raw: string): string {
  const text = raw.trim();
  if (!text) return '';
  let host: string;
  try {
    host = new URL(text.includes('://') ? text : `ftp://${text}`).hostname.toLowerCase();
  } catch {
    return '';
  }
  const ipv6 = /^\[([0-9a-f:.]+)\]$/.exec(host);
  if (ipv6) return ipv6[1];
  return /^[a-z0-9.-]+$/.test(host) ? host : '';
}

export function Accounts() {
  const { t } = useT();
  const { toast } = useToast();
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [catalogue, setCatalogue] = useState<CatalogueService[]>([]);
  const [loadError, setLoadError] = useState(false);
  const [dialog, setDialog] = useState<DialogState | null>(null);
  // The row awaiting confirmation; the only path to removeAccountCredential.
  const [confirming, setConfirming] = useState<Account | null>(null);
  const [refreshing, setRefreshing] = useState<ReadonlySet<string>>(new Set());
  const [loginHosts, setLoginHosts] = useState('');
  // Bumped on every change to the Usenet servers, whose row on the priority
  // card comes and goes with them.
  const [usenetSaves, setUsenetSaves] = useState(0);
  // The two cards that keep their own windows open the add one when the
  // floating action asks for it.
  const [adding, setAdding] = useState<'usenet' | 'hoster' | null>(null);
  const [picking, setPicking] = useState(false);
  const hoster = useHosterLogins(setLoginHosts);

  const load = useCallback(async () => {
    try {
      const [a, c] = await Promise.all([fetchAccounts(), fetchAccountCatalogue()]);
      setAccounts(a);
      setCatalogue(c);
      setLoadError(false);
    } catch {
      setLoadError(true);
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), HEALTH_POLL_MS);
    return () => window.clearInterval(timer);
  }, [load]);

  async function onRefresh(a: Account) {
    setRefreshing((s) => new Set(s).add(a.id));
    try {
      const updated = await testAccount(a.service, a.account);
      setAccounts((cur) => cur?.map((x) => (x.id === a.id ? updated : x)) ?? cur);
    } catch {
      toast(t('common.loadFailed'), 'fail');
    } finally {
      setRefreshing((s) => {
        const next = new Set(s);
        next.delete(a.id);
        return next;
      });
    }
  }

  async function onToggle(a: Account, enabled: boolean) {
  // Optimistic: a spinner over a toggle reads as broken.
    setAccounts((cur) => cur?.map((x) => (x.id === a.id ? { ...x, enabled } : x)) ?? cur);
    try {
      await setAccountEnabled(a.service, a.account, enabled);
    } catch {
      toast(t('common.loadFailed'), 'fail');
      await load();
    }
  }

  // Confirmed first, since the key cannot be put back.
  async function doRemove(a: Account) {
    setConfirming(null);
    try {
      await removeAccountCredential(a.service, a.account);
      toast(t('accounts.removed'), 'info');
      await load();
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
  }

  function onEdit(a: Account) {
    const svc = catalogue.find((s) => s.id === a.service);
    if (svc?.group === 'remoteServer') {
      setDialog({ mode: 'server', service: svc, host: a.account });
      return;
    }
    setDialog({ mode: 'edit', service: a.service, account: a.account });
  }

  if (accounts === null) {
    return loadError ? (
      <ErrorCard message={t('common.loadFailed')} retry={() => void load()} retryLabel={t('common.retry')} />
    ) : (
      <LoadingCard label={t('common.loading')} />
    );
  }

  const byId = new Map(catalogue.map((s) => [s.id, s]));
  // Hoster logins come from internal/hosterauth (HosterLoginSection), not the
  // catalogue, since any host JDownloader knows can have one.
  const debridIds = new Set(catalogue.filter((s) => s.group === 'debrid').map((s) => s.id));
  const debridRows = accounts.filter((a) => debridIds.has(a.service));
  const solverIds = new Set(catalogue.filter((s) => s.group === 'captchaSolver').map((s) => s.id));
  const solverRows = accounts.filter((a) => solverIds.has(a.service));
  const serverService = catalogue.find((s) => s.group === 'remoteServer');
  const serverRows = accounts.filter((a) => a.service === serverService?.id);

  const labelOf = (a: Account) => byId.get(a.service)?.label ?? a.service;

  const tableProps = {
    catalogue: byId,
    refreshing,
    onRefresh,
    onToggle,
    onRemove: (a: Account) => setConfirming(a),
    onEdit,
  };

  // Every empty card sends the reader to the floating action by its name.
  const addPointer = t('accounts.addPointer', { button: t('accounts.newAccount') });
  const captchaOrder = t('settings.captcha.orderTitle');
  const kinds: (Kind & { open: () => void })[] = [
    {
      id: 'debrid',
      name: t('accounts.kind.debrid'),
      sub: t('accounts.kind.debridSub'),
      icon: <IconAccounts />,
      open: () => setDialog({ mode: 'new', group: 'debrid' }),
    },
    {
      id: 'usenet',
      name: t('accounts.kind.usenet'),
      sub: t('accounts.kind.usenetSub'),
      icon: <IconGlobe />,
      open: () => setAdding('usenet'),
    },
    {
      id: 'hoster',
      name: t('accounts.kind.hoster'),
      sub: t('accounts.kind.hosterSub'),
      icon: <IconKey />,
      open: () => setAdding('hoster'),
    },
    ...(serverService
      ? [
          {
            id: 'server',
            name: t('accounts.kind.server'),
            sub: t('accounts.kind.serverSub'),
            icon: <IconFleet />,
            open: () => setDialog({ mode: 'server', service: serverService, host: null }),
          },
        ]
      : []),
    {
      id: 'captcha',
      name: t('accounts.kind.captcha'),
      sub: t('accounts.kind.captchaSub'),
      icon: <IconCaptcha />,
      open: () => setDialog({ mode: 'new', group: 'captchaSolver' }),
    },
  ];

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('accounts.title')} />

      <Card hue={0} data-new="import-gone" className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.debrid.hint')}>
          {t('accounts.debrid.title')}
        </SectionTitle>
        {debridRows.length > 0 ? (
          <AccountsTable label={t('accounts.debrid.title')} rows={debridRows} {...tableProps} />
        ) : (
          <EmptyState
            nested
            icon={<IconAccounts width={26} height={26} />}
            title={t('accounts.debrid.empty')}
            hint={`${t('accounts.debrid.emptyHint')} ${addPointer}`}
          />
        )}
      </Card>

      <Card hue={1} className="flex flex-col gap-3">
        <UsenetServersSection
          hue={1}
          onChanged={() => setUsenetSaves((n) => n + 1)}
          emptyPointer={addPointer}
          adding={adding === 'usenet'}
          onAddClose={() => setAdding(null)}
        />
      </Card>

      <Card hue={2} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.hoster.hint')}>
          {t('accounts.hoster.title')}
        </SectionTitle>
        <HosterLoginSection
          data={hoster}
          emptyPointer={addPointer}
          adding={adding === 'hoster'}
          onAddClose={() => setAdding(null)}
        />
      </Card>

      {serverService && (
        <Card hue={3} className="flex flex-col gap-3">
          <SectionTitle hint={t('accounts.server.hint')}>{t('accounts.server.title')}</SectionTitle>
          {serverRows.length > 0 ? (
            <AccountsTable label={t('accounts.server.title')} rows={serverRows} {...tableProps} />
          ) : (
            <EmptyState
              nested
              icon={<IconFleet width={26} height={26} />}
              title={t('accounts.server.empty')}
              hint={`${t('accounts.server.emptyHint')} ${addPointer}`}
            />
          )}
        </Card>
      )}

      {/* A solver's key unlocks no link, so these rows have no import and
          nothing to renew. The order they are tried in is the captcha
          priority card below. */}
      <Card hue={4} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.captcha.hint', { order: captchaOrder })}>{t('accounts.captcha.title')}</SectionTitle>
        {solverRows.length > 0 ? (
          <AccountsTable label={t('accounts.captcha.title')} rows={solverRows} {...tableProps} />
        ) : (
          <EmptyState
            nested
            icon={<IconCaptcha width={26} height={26} />}
            title={t('accounts.captcha.empty')}
            hint={`${t('accounts.captcha.emptyHint', { order: captchaOrder })} ${addPointer}`}
          />
        )}
      </Card>

      {/* The signature, so RoutingSection looks again only when the set of
          services or switched-on logins changes, not on every poll. The
          captcha card stands outside the settings shell, so it saves as it
          goes. */}
      <RoutingSection
        accounts={accounts}
        catalogue={catalogue}
        emptyPointer={addPointer}
        signature={`${(accounts ?? []).map((a) => a.service).sort().join(',')}|${loginHosts}|${usenetSaves}`}
        captcha={
          <LiveSettings>
            <CaptchaPriorityCard hue={7} accounts={accounts} catalogue={catalogue} />
          </LiveSettings>
        }
      />

      <PageActions>
        <PageAction
          primary
          icon={<IconAdd />}
          label={t('accounts.newAccount')}
          onClick={() => setPicking(true)}
          data-new="accounts-add"
        />
      </PageActions>

      {/* The kind leads on to the window that kind already has, the services
          to pick from or the form, so each is still added in one place. The
          tiles follow the cards on this page, top to bottom. */}
      {picking && (
        <Modal
          title={t('accounts.newAccount')}
          onClose={() => setPicking(false)}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="secondary"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                onClick={() => setPicking(false)}
              />
            </>
          }
        >
          <KindPicker
            kinds={kinds}
            onPick={(id) => {
              setPicking(false);
              kinds.find((k) => k.id === id)?.open();
            }}
          />
        </Modal>
      )}

      {/* The windows below belong to a card, so they wear its colour. */}
      {dialog?.mode === 'server' && (
        <ServerDialog
          service={dialog.service}
          host={dialog.host}
          onClose={() => setDialog(null)}
          onSaved={load}
        />
      )}
      {dialog && dialog.mode !== 'server' && (
        <CredentialDialog
          mode={dialog.mode}
          group={
            dialog.mode === 'new'
              ? dialog.group
              : byId.get(dialog.service)?.group === 'captchaSolver'
                ? 'captchaSolver'
                : 'debrid'
          }
          initial={dialog.mode === 'edit' ? { service: dialog.service, account: dialog.account } : undefined}
          catalogue={catalogue}
          accounts={accounts}
          onClose={() => setDialog(null)}
          onSaved={load}
        />
      )}

      {confirming && (
        <Modal
          title={t('accounts.remove')}
          hue={0}
          onClose={() => setConfirming(null)}
          footer={
            <>
              {/* The spacer puts the pair at the end, the commit last; JSX order,
                  so it mirrors in right-to-left languages. */}
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                onClick={() => setConfirming(null)}
              />
              <Button kind="ghost" icon={<IconTrash width={16} height={16} />} onClick={() => void doRemove(confirming)}>
                {t('accounts.remove')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">
            {/* The account id tells two rows of one service apart. */}
            {t('accounts.removeConfirm', {
              name: confirming.account ? `${labelOf(confirming)} · ${confirming.account}` : labelOf(confirming),
            })}
          </p>
        </Modal>
      )}
    </div>
  );
}

interface TableActions {
  catalogue: Map<string, CatalogueService>;
  refreshing: ReadonlySet<string>;
  onRefresh: (a: Account) => void;
  onToggle: (a: Account, enabled: boolean) => void;
  onRemove: (a: Account) => void;
  onEdit: (a: Account) => void;
}

function AccountsTable({
  label,
  rows,
  catalogue,
  refreshing,
  onRefresh,
  onToggle,
  onRemove,
  onEdit,
}: TableActions & { label: string; rows: Account[] }) {
  const { t } = useT();
  return (
    <AccountTable
      label={label}
      rows={rows.map((a): AccountRow => {
        const svc = catalogue.get(a.service);
        // An own server's row is its hostname. KnightLoader has no way to check
        // that login apart from a download, so the row has nothing to refresh.
        const server = svc?.group === 'remoteServer';
        // A solver's key has no plan to renew; its row menu only checks it.
        const renewable = svc?.group !== 'captchaSolver';
        return {
          key: a.id,
          // The service's icon, from the host of its "where do I get a key"
          // link; an own server shows its own.
          iconHost: server ? a.account : (svc?.whereUrl ?? ''),
          label: server ? a.account : (svc?.label ?? a.service),
          enabled: a.enabled,
          status: server ? <ServerStatus /> : <AccountStatus account={a} busy={refreshing.has(a.id)} />,
          tier: a.tier,
          expiry: a.expiry,
          traffic: a.traffic,
          onToggle: (v) => onToggle(a, v),
          onEdit: () => onEdit(a),
          // A credential from the container's environment cannot be removed here.
          onRemove: a.fromEnv ? undefined : () => onRemove(a),
          menu: server
            ? undefined
            : [
                {
                  id: 'actions',
                  items: [
                    {
                      id: 'refresh',
                      label: t('accounts.refresh'),
                      icon: <IconRefresh width={16} height={16} />,
                      onSelect: () => onRefresh(a),
                    },
                    ...(renewable
                      ? [
                          {
                            id: 'renew',
                            label: a.expiry ? t('accounts.renew') : t('accounts.buyPremium'),
                            icon: <IconExternalLink width={16} height={16} />,
                            // Only with an expiry and somewhere to renew.
                            disabled: !a.expiry || !svc?.whereUrl,
                            onSelect: () => {
                              if (svc?.whereUrl) openExternal(svc.whereUrl);
                            },
                          },
                        ]
                      : []),
                  ],
                },
              ],
        };
      })}
    />
  );
}

function AccountStatus({ account, busy }: { account: Account; busy: boolean }) {
  const { t } = useT();
  if (busy) {
    return (
      <span className="inline-flex items-center gap-1.5 text-meta font-medium text-carbon-textMuted">
        {/* The house's live dot rather than animate-spin, so it follows the
            motion level and reduced motion. */}
        <span
          aria-hidden
          className="glim-live h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] bg-accent"
        />
        {t('accounts.refreshing')}
      </span>
    );
  }
  // Never checked yet differs from a failed check.
  if (!account.detail) {
    return (
      <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusNeutral">
        <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusNeutralSolid" />
        {t('accounts.unchecked')}
      </span>
    );
  }
  if (account.ok) {
    return (
      <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusOk">
        <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusOkSolid" />
        {t('accounts.ok')}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusFail">
      <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusFailSolid" />
      {t('accounts.failed')}
      <InfoBubble tip={account.detail} />
    </span>
  );
}

/** ServerStatus is an own server's status: its login is only tried by a download. */
function ServerStatus() {
  const { t } = useT();
  return (
    <span className="inline-flex items-center gap-1.5 text-meta font-medium text-statusNeutral">
      <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusNeutralSolid" />
      {t('accounts.unchecked')}
      <InfoBubble tip={t('accounts.server.uncheckedHint')} />
    </span>
  );
}

/**
 * ServerDialog adds or changes the login for one of the user's own servers.
 * The hostname is the account id, and it cannot change once stored: a login
 * under another name is a different server.
 */
function ServerDialog({
  service,
  host,
  onClose,
  onSaved,
}: {
  service: CatalogueService;
  host: string | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [hostInput, setHostInput] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [saving, setSaving] = useState(false);

  const target = host ?? serverHost(hostInput);
  const filled = target !== '' && username.trim() !== '' && password.trim() !== '';

  // Saved without a check: checking would mean a login on the server, which
  // only a download does.
  async function save() {
    setSaving(true);
    try {
      await saveAccountCredential(service.id, target, { username, password });
      toast(t('accounts.saved'), 'ok');
      await onSaved();
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title={host ? t('accounts.editCredentialTitle', { service: host }) : t('accounts.server.add')}
      hint={host ? undefined : t('accounts.server.hint')}
      hue={3}
      onClose={onClose}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <Button onClick={() => void save()} disabled={saving || !filled}>
            {saving ? t('accounts.saving') : t('accounts.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {host === null && (
          <Field label={t('accounts.server.hostField')} hint={t('accounts.server.hostHint')}>
            <TextInput
              autoComplete="off"
              spellCheck={false}
              value={hostInput}
              onChange={(e) => setHostInput(e.target.value)}
            />
          </Field>
        )}
        <Field label={t('accounts.usernameField')}>
          <TextInput autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label={t('accounts.passwordField')}>
          <PasswordInput
            autoComplete="new-password"
            value={password}
            onChange={setPassword}
            showLabel={t('common.showPassword')}
            hideLabel={t('common.hidePassword')}
          />
        </Field>
      </div>
    </Modal>
  );
}

function CredentialDialog({
  mode,
  group,
  initial,
  catalogue,
  accounts,
  onClose,
  onSaved,
}: {
  mode: 'new' | 'edit';
  /** The card the dialog was opened from, which decides what it offers. */
  group: AccountGroup;
  initial?: { service: string; account: string };
  catalogue: CatalogueService[];
  accounts: Account[];
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { t } = useT();
  const { toast } = useToast();

  const editingRow = initial ? accounts.find((a) => a.service === initial.service && a.account === initial.account) : undefined;

  const [picked, setPicked] = useState<CatalogueService | null>(() =>
    initial ? (catalogue.find((s) => s.id === initial.service) ?? null) : null,
  );
  const [accountId, setAccountId] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [verifyResult, setVerifyResult] = useState<VerifyResult | null>(null);
  const [verifying, setVerifying] = useState(false);
  const [saving, setSaving] = useState(false);
  const [query, setQuery] = useState('');

  const fromEnv = editingRow?.fromEnv ?? false;
  const hasDefault = (id: string) => accounts.some((a) => a.service === id && a.account === '');
  const services = catalogue.filter((s) => s.group === group);
  // A solver has one key, the one the captcha page's order reads, so picking a
  // solver that has one already changes that key rather than adding a second.
  const solver = group === 'captchaSolver';
  const replacing = solver && mode === 'new' && picked !== null && hasDefault(picked.id);
  const editing = mode === 'edit' || replacing;

  function credential(): AccountCredential {
    if (!picked) return {};
    return picked.kind === 'apiKey' ? { apiKey } : { username, password };
  }

  function credentialFilled(): boolean {
    if (!picked) return false;
    return picked.kind === 'apiKey' ? apiKey.trim() !== '' : username.trim() !== '' && password.trim() !== '';
  }

  async function doSave(force: boolean) {
    if (!picked) return;
    const account = editing ? (initial?.account ?? '') : accountId.trim();
    setSaving(true);
    try {
      if (!force) {
        setVerifying(true);
        const result = await verifyAccountCredential(picked.id, account, credential());
        setVerifying(false);
        setVerifyResult(result);
        // A failed check stops here; "save anyway" takes a second click.
        if (!result.ok) {
          setSaving(false);
          return;
        }
      }
      await saveAccountCredential(picked.id, account, credential());
      toast(t('accounts.saved'), 'ok');
      await onSaved();
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'fail');
    } finally {
      setSaving(false);
      setVerifying(false);
    }
  }

  const accountIdRequired = !editing && picked !== null && hasDefault(picked.id) && accountId.trim() === '';

  // Named after the card, its add button and the row's edit action, so every
  // step of the way reads as the same thing.
  const title = !picked
    ? t(solver ? 'accounts.captcha.pick' : 'accounts.pickDebridAccount')
    : editing
      ? t('accounts.editCredentialTitle', { service: picked.label })
      : t('accounts.addAccountTitle', { service: picked.label });

  // A tile's second line says where the brand lives, or that it has a key
  // already, which stays in view.
  const brands: Brand[] = services
    .map((s) => {
      const connected = hasDefault(s.id);
      return {
        id: s.id,
        name: s.label,
        host: s.whereUrl,
        sub: connected ? t('accounts.connected') : siteOf(s.whereUrl),
        held: connected,
      };
    })
    .sort((x, y) => x.name.localeCompare(y.name));

  return (
    <Modal
      title={title}
      // While adding, the window says what the card it fills is for.
      hint={
        mode === 'new'
          ? solver
            ? t('accounts.captcha.hint', { order: t('settings.captcha.orderTitle') })
            : t('accounts.debrid.hint')
          : undefined
      }
      hue={solver ? 4 : 0}
      // The picker's window, which the form of the picked brand stays in.
      wide={mode === 'new'}
      height={picked ? 'content' : 'capped'}
      holdHeight={!picked && query !== ''}
      right={!picked && <PickerSearch value={query} onChange={setQuery} placeholder={t('search.placeholder')} />}
      onClose={onClose}
      footer={
        picked && !fromEnv ? (
          <>
            {/* A new account's form leads back to the list it was picked from;
                an edit has no list behind it and offers the way out. */}
            {mode === 'new' ? (
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
            ) : (
              <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
            )}
            <span className="flex-1" />
            {verifyResult && !verifyResult.ok && (
              <Button kind="secondary" onClick={() => void doSave(true)} disabled={saving}>
                {t('accounts.saveAnyway')}
              </Button>
            )}
            <Button onClick={() => void doSave(false)} disabled={saving || !credentialFilled() || accountIdRequired}>
              {verifying ? t('accounts.verifying') : saving ? t('accounts.saving') : t('accounts.save')}
            </Button>
          </>
        ) : (
          // The service picker and an account set by the environment have
          // nothing to save, so the way out stands alone.
          <>
            <span className="flex-1" />
            <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          </>
        )
      }
    >
      {!picked ? (
        <BrandPicker
          brands={brands.filter((b) => brandMatches(b, query))}
          none={t('accounts.noServicesFound')}
          onPick={(id) => {
            setPicked(services.find((s) => s.id === id) ?? null);
            setAccountId('');
            setVerifyResult(null);
          }}
        />
      ) : (
        // The form keeps a reading width in the picker's wide window.
        <div className="flex max-w-md flex-col gap-4">
          {fromEnv ? (
            <p className="text-sm text-carbon-textSub">{t('accounts.credentialFromEnv', { env: editingRow?.envVar ?? '' })}</p>
          ) : (
            <>
              {!editing && hasDefault(picked.id) && (
                <Field label={t('accounts.accountLabel')} hint={t('accounts.accountLabelHint')}>
                  <TextInput
                    value={accountId}
                    onChange={(e) => setAccountId(e.target.value)}
                    placeholder={t('accounts.accountLabelPlaceholder')}
                  />
                </Field>
              )}

              {/* PasswordInput, so a pasted key can be revealed and read back. */}
              {picked.kind === 'apiKey' ? (
                <Field label={t('accounts.keyLabel', { service: picked.label })} hint={t('accounts.keyHint')}>
                  <PasswordInput
                    autoComplete="off"
                    value={apiKey}
                    onChange={setApiKey}
                    showLabel={t('common.showPassword')}
                    hideLabel={t('common.hidePassword')}
                  />
                </Field>
              ) : (
                <>
                  <Field label={t(picked.userLabel ? FIELD_LABELS[picked.userLabel] : 'accounts.usernameField')}>
                    <TextInput autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
                  </Field>
                  <Field label={t(picked.passLabel ? FIELD_LABELS[picked.passLabel] : 'accounts.passwordField')}>
                    <PasswordInput
                      autoComplete="new-password"
                      value={password}
                      onChange={setPassword}
                      showLabel={t('common.showPassword')}
                      hideLabel={t('common.hidePassword')}
                    />
                  </Field>
                </>
              )}

              {picked.whereUrl && (
                <LinkBadge href={picked.whereUrl} title={t('accounts.whereToFind')} className="self-start" />
              )}

              {verifyResult && (
                <p className={`text-xs ${verifyResult.ok ? 'text-statusOk' : 'text-statusFail'}`} role="status">
                  {/* A solver unlocks no hoster, so it has no count to add. */}
                  {verifyResult.ok
                    ? solver
                      ? t('accounts.ok')
                      : `${t('accounts.ok')} · ${verifyResult.hosts} ${t('accounts.hosts')}`
                    : t('accounts.verifyFailed', { detail: verifyResult.detail })}
                </p>
              )}
            </>
          )}
        </div>
      )}
    </Modal>
  );
}

// Routing: the resolver priority order, which is no account: it belongs to the
// registry (internal/resolver).

/** The id prefix of a hoster login's row (app.loginRowID). */
const LOGIN_ROW = 'login:';
/** The rows of backends that come with KnightLoader rather than with an account. */
const BUILT_IN = ['jd', 'ytdlp', 'direct', 'torrent'];

/**
 * connectedDebrid maps each debrid service with a connected, switched-on
 * account to why its accounts do not work, '' while one of them does. A
 * failed check leaves the account in the order, since it is still asked.
 */
function connectedDebrid(accounts: Account[], catalogue: Map<string, CatalogueService>): Map<string, string> {
  const out = new Map<string, string>();
  for (const a of accounts) {
    if (catalogue.get(a.service)?.group !== 'debrid' || !a.configured || !a.enabled) continue;
    const failed = !!a.detail && !a.ok;
    const before = out.get(a.service);
    if (!failed) out.set(a.service, '');
    else if (before === undefined) out.set(a.service, a.detail);
  }
  return out;
}

/**
 * ladderOf is the order the instance reports with every connected debrid
 * service in it. One the instance has not wired yet goes last, so the picture
 * never leaves out an account the cards above it list.
 */
function ladderOf(priority: ResolverInfo[], debrid: Map<string, string>): string[] {
  const ids = priority.map((r) => r.id);
  return [...ids, ...[...debrid.keys()].filter((id) => !ids.includes(id))];
}

/**
 * What a row takes. A debrid service's row shares one sentence with its name
 * in it. The torrent row says that the debrid services above it may take its
 * links.
 */
const ROW_TIPS: Partial<Record<string, TranslationKey>> = {
  jd: 'accounts.routing.tip.jd',
  ytdlp: 'accounts.routing.tip.ytdlp',
  direct: 'accounts.routing.tip.direct',
  torrent: 'accounts.routing.tip.torrent',
  nntp: 'accounts.routing.tip.nntp',
};

function RoutingSection({
  accounts,
  catalogue,
  signature,
  emptyPointer,
  captcha,
}: {
  accounts: Account[];
  catalogue: CatalogueService[];
  signature: string;
  /** The way to the floating action that adds an account, for the card without one. */
  emptyPointer: string;
  /** The captcha priority card, which stands beside the download one. */
  captcha: ReactNode;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [priority, setPriority] = useState<ResolverInfo[] | null>(null);
  const [busy, setBusy] = useState(false);

  // Re-read whenever the configured services or the switched-on hoster logins
  // change: saving a debrid key registers a resolver at once, and each login
  // is a row of its own.
  useEffect(() => {
    let live = true;
    void fetchResolverPriority().then((p) => live && setPriority(p));
    return () => {
      live = false;
    };
  }, [signature]);

  const byId = new Map(catalogue.map((s) => [s.id, s]));
  const labelFor = (id: string) => {
    if (id.startsWith(LOGIN_ROW)) return id.slice(LOGIN_ROW.length);
    return byId.get(id)?.label ?? resolverLabel(id, t);
  };
  const tipFor = (id: string) => {
    if (byId.get(id)?.group === 'debrid') return t('accounts.routing.tip.debrid', { name: labelFor(id) });
    const tip = id.startsWith(LOGIN_ROW) ? 'accounts.routing.tip.login' : ROW_TIPS[id];
    return tip && t(tip);
  };

  const debrid = connectedDebrid(accounts, byId);
  const ids = priority && ladderOf(priority, debrid);
  const troubleFor = (id: string) => {
    const failed = debrid.get(id);
    return failed && t('accounts.routing.checkFailed', { reason: failed });
  };

  // "Automatisch" stores an empty order, which clears the stored one, so the
  // order follows the ladder as it changes instead of freezing today's.
  async function store(next: string[]) {
    setBusy(true);
    try {
      setPriority(await saveResolverPriority(next));
    } catch (e) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
    } finally {
      setBusy(false);
    }
  }

  const title = t('accounts.routing.priorityTitle');
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      {/* The state of the backend that loads for free is on the Health page,
          and its revision on the card of built-in parts. */}
      <div data-new="free-first" className="sm:col-span-2">
        <FreeDownloadsCard hue={5} />
      </div>

      <PriorityCard
        hue={6}
        data-new="priority-flow"
        title={title}
        hint={`${t('accounts.routing.orderHint')}

${t('accounts.routing.orderHintTorrents')}`}
        intro={t('accounts.routing.priorityIntro')}
      >
        {ids === null ? (
          <p className="text-sm text-carbon-textMuted">{t('common.loading')}</p>
        ) : (
          <div data-new="priority-accounts" className="flex flex-col gap-3">
            {ids.every((id) => BUILT_IN.includes(id)) ? (
              // The built-in backends alone are no order worth setting.
              <EmptyState
                nested
                icon={<IconAccounts width={26} height={26} />}
                title={t('accounts.routing.priorityNone')}
                hint={emptyPointer}
              />
            ) : (
              <>
                <div data-new="priority-tips">
                  <PriorityLadder
                    ids={ids}
                    title={title}
                    entry={t('accounts.routing.priorityEntry')}
                    labelFor={labelFor}
                    tipFor={tipFor}
                    troubleFor={troubleFor}
                    busy={busy}
                    onReorder={store}
                  />
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    kind="secondary"
                    disabled={busy}
                    hint={t('accounts.routing.priorityAutoHint')}
                    onClick={() => void store([])}
                  >
                    {t('accounts.routing.priorityAuto')}
                  </Button>
                </div>
              </>
            )}
          </div>
        )}
      </PriorityCard>

      {captcha}
    </div>
  );
}
