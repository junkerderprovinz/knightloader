// The accounts page: one row per configured service and account, as read from
// internal/accounts/catalogue.go and internal/app/app_accounts.go. Debrid
// accounts come first and hoster logins below; the section follows the
// catalogue's Group field, and both use the same AccountsTable.
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEventHandler,
  type PointerEventHandler,
} from 'react';
import {
  type Account,
  type AccountCredential,
  type CatalogueService,
  type JDStatus,
  type ResolverInfo,
  type VerifyResult,
  fetchAccounts,
  fetchAccountCatalogue,
  fetchJDStatus,
  fetchResolverPriority,
  removeAccountCredential,
  saveAccountCredential,
  saveResolverPriority,
  setAccountEnabled,
  testAccount,
  verifyAccountCredential,
} from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { fmtDate } from '../lib/format';
import {
  Button,
  Card,
  EmptyState,
  ErrorCard,
  Field,
  InfoBubble,
  LoadingCard,
  Modal,
  PageHeader,
  PasswordInput,
  SectionTitle,
  TextInput,
  Toggle,
} from '../components/ui';
import { AccountTable } from '../components/AccountTable';
import { HosterLoginSection } from '../components/HosterLoginSection';
import {
  IconAccounts,
  IconClose,
  IconGrip,
  IconExternalLink,
  IconPlus,
  IconRetry,
  IconTrash,
} from '../lib/icons';
import { HosterIcon } from '../components/HosterIcon';

// Reads what the background account-health refresher stored; expiry and
// traffic change in hours.
const HEALTH_POLL_MS = 30000;

type DialogState = { mode: 'new' } | { mode: 'edit'; service: string; account: string };

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

  const labelOf = (a: Account) => byId.get(a.service)?.label ?? a.service;

  const tableProps = {
    catalogue: byId,
    refreshing,
    onRefresh,
    onToggle,
    onRemove: (a: Account) => setConfirming(a),
    onEdit,
  };

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('accounts.title')} />

      <Card hue={0} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.debrid.hint')}>
          {t('accounts.debrid.title')}
        </SectionTitle>
        {debridRows.length > 0 ? (
          <>
            <AccountsTable rows={debridRows} {...tableProps} />
            <Button
              kind="secondary"
              hue={0}
              icon={<IconPlus width={16} height={16} />}
              className="self-start"
              onClick={() => setDialog({ mode: 'new' })}
            >
              {t('accounts.newAccount')}
            </Button>
          </>
        ) : (
          <EmptyState
            nested
            icon={<IconAccounts width={26} height={26} />}
            title={t('accounts.debrid.empty')}
            hint={t('accounts.debrid.emptyHint')}
            action={
              <Button kind="secondary" hue={0} icon={<IconPlus width={16} height={16} />} onClick={() => setDialog({ mode: 'new' })}>
                {t('accounts.newAccount')}
              </Button>
            }
          />
        )}
      </Card>

      <Card hue={1} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.hoster.hint')}>
          {t('accounts.hoster.title')}
        </SectionTitle>
        <HosterLoginSection onEnabledHosts={setLoginHosts} />
      </Card>

      {/* The signature, so RoutingSection looks again only when the set of
          services or switched-on logins changes, not on every poll. */}
      <RoutingSection
        catalogue={catalogue}
        signature={`${(accounts ?? []).map((a) => a.service).sort().join(',')}|${loginHosts}`}
      />

      {dialog && (
        <CredentialDialog
          mode={dialog.mode}
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

function AccountsTable({ rows, catalogue, refreshing, onRefresh, onToggle, onRemove, onEdit }: TableActions & { rows: Account[] }) {
  const { t } = useT();
  return (
    <AccountTable
      label={t('accounts.debrid.title')}
      rows={rows.map((a) => {
        const svc = catalogue.get(a.service);
        return {
          key: a.id,
          // The service's icon, from the host of its "where do I get a key" link.
          iconHost: svc?.whereUrl ?? '',
          label: svc?.label ?? a.service,
          enabled: a.enabled,
          status: <AccountStatus account={a} busy={refreshing.has(a.id)} />,
          tier: a.tier,
          expiry: a.expiry,
          traffic: a.traffic,
          onToggle: (v) => onToggle(a, v),
          onEdit: () => onEdit(a),
          // A credential from the container's environment cannot be removed here.
          onRemove: a.fromEnv ? undefined : () => onRemove(a),
          menu: [
            {
              id: 'actions',
              items: [
                {
                  id: 'refresh',
                  label: t('accounts.refresh'),
                  icon: <IconRetry width={16} height={16} />,
                  onSelect: () => onRefresh(a),
                },
                {
                  id: 'renew',
                  label: a.expiry ? t('accounts.renew') : t('accounts.buyPremium'),
                  icon: <IconExternalLink width={16} height={16} />,
                  // Only with an expiry and somewhere to renew.
                  disabled: !a.expiry || !svc?.whereUrl,
                  onSelect: () => {
                    if (svc?.whereUrl) window.open(svc.whereUrl, '_blank', 'noopener,noreferrer');
                  },
                },
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
      <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-carbon-textMuted">
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
      <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusNeutral">
        <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusNeutralSolid" />
        {t('accounts.unchecked')}
      </span>
    );
  }
  if (account.ok) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusOk">
        <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusOkSolid" />
        {t('accounts.ok')}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-statusFail">
      <span className="h-1.5 w-1.5 rounded-[var(--radius-pill)] bg-statusFailSolid" />
      {t('accounts.failed')}
      <InfoBubble tip={account.detail} />
    </span>
  );
}

function CredentialDialog({
  mode,
  initial,
  catalogue,
  accounts,
  onClose,
  onSaved,
}: {
  mode: 'new' | 'edit';
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

  const fromEnv = editingRow?.fromEnv ?? false;
  const hasDefault = (id: string) => accounts.some((a) => a.service === id && a.account === '');
  // Debrid only; the captcha solvers in the catalogue are set on the Captcha
  // page.
  const debridServices = catalogue.filter((s) => s.group === 'debrid');

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
    const account = mode === 'new' ? accountId.trim() : (initial?.account ?? '');
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

  const accountIdRequired = mode === 'new' && picked !== null && hasDefault(picked.id) && accountId.trim() === '';

  const title = !picked
    ? t('accounts.pickService')
    : mode === 'edit'
      ? t('accounts.editTitle', { service: picked.label })
      : t('accounts.newAccountTitle');

  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        picked && !fromEnv ? (
          <>
            <span className="flex-1" />
            <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
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
        <ServicePicker
          services={debridServices}
          hasDefault={hasDefault}
          onPick={(s) => {
            setPicked(s);
            setAccountId('');
            setVerifyResult(null);
          }}
        />
      ) : (
        <div className="flex flex-col gap-4">
          {mode === 'new' && (
            <button
              type="button"
              onClick={() => setPicked(null)}
              className="self-start text-xs text-carbon-textMuted hover:text-carbon-text"
            >
              {t('accounts.changeService')}
            </button>
          )}

          {fromEnv ? (
            <p className="text-sm text-carbon-textSub">{t('accounts.credentialFromEnv', { env: editingRow?.envVar ?? '' })}</p>
          ) : (
            <>
              {mode === 'new' && hasDefault(picked.id) && (
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
                </>
              )}

              {picked.whereUrl && (
                <a
                  href={picked.whereUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="self-start text-[11px] text-carbon-textMuted underline-offset-2 hover:text-carbon-text hover:underline"
                >
                  {t('accounts.whereToFind')}
                </a>
              )}

              {verifyResult && (
                <p className={`text-xs ${verifyResult.ok ? 'text-statusOk' : 'text-statusFail'}`} role="status">
                  {verifyResult.ok
                    ? `${t('accounts.ok')} · ${verifyResult.hosts} ${t('accounts.hosts')}`
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

function ServicePicker({
  services,
  hasDefault,
  onPick,
}: {
  services: CatalogueService[];
  hasDefault: (id: string) => boolean;
  onPick: (s: CatalogueService) => void;
}) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3">
      <div className="flex max-h-72 flex-col gap-1 overflow-y-auto">
        {services.map((s) => (
          <button
            key={s.id}
            type="button"
            onClick={() => onPick(s)}
            className="flex items-center gap-3 rounded-[var(--radius-control)] px-3 py-2 text-start hover:bg-carbon-hover"
          >
            <span className="min-w-0 flex-1">
              {/* The service's icon, as in the table. */}
              <span className="flex items-center gap-2 text-sm text-carbon-text">
                <HosterIcon host={s.whereUrl} />
                {s.label}
              </span>
            </span>
            {hasDefault(s.id) && <span className="glim-eyebrow shrink-0">{t('accounts.connected')}</span>}
          </button>
        ))}
      </div>
    </div>
  );
}

// Routing: the resolver priority order and the JD sidecar's status. Neither is
// an account: the order belongs to the registry (internal/resolver), and the
// sidecar is configured by KL_JD without a credential.

/** Locale keys for resolvers whose label is a descriptive phrase. */
const RESOLVER_LABEL_KEYS: Partial<Record<string, TranslationKey>> = {
  direct: 'accounts.routing.resolver.direct',
  http: 'accounts.routing.resolver.http',
  torrent: 'accounts.routing.resolver.torrent',
  hostheaders: 'accounts.routing.resolver.hostheaders',
  remotefs: 'accounts.routing.resolver.remotefs',
};

/** Resolvers named after a product, which stay untranslated. */
const RESOLVER_PROPER_NAMES: Record<string, string> = {
  ytdlp: 'yt-dlp',
  jd: 'JDownloader',
};

/** The id prefix of a hoster login's row (app.loginRowID). */
const LOGIN_ROW = 'login:';

/**
 * The resolvers that decide per link and stay off the ordered list
 * (app.perLinkResolvers), shown below it so the card still says where a link
 * goes once no listed service takes it.
 */
const AUTOMATIC_ROWS: { id: string; what: TranslationKey }[] = [
  { id: 'jd', what: 'accounts.routing.automatic.jd' },
  { id: 'ytdlp', what: 'accounts.routing.automatic.ytdlp' },
  { id: 'direct', what: 'accounts.routing.automatic.direct' },
];

function RoutingSection({ catalogue, signature }: { catalogue: CatalogueService[]; signature: string }) {
  const { t } = useT();
  const [priority, setPriority] = useState<ResolverInfo[] | null>(null);
  const [jd, setJd] = useState<JDStatus | null>(null);

  // Re-read whenever the configured services or the switched-on hoster logins
  // change: saving a debrid key registers a resolver at once, and each login
  // is a row of its own.
  useEffect(() => {
    let live = true;
    void fetchResolverPriority().then((p) => live && setPriority(p));
    void fetchJDStatus().then((s) => live && setJd(s));
    return () => {
      live = false;
    };
  }, [signature]);

  const byId = new Map(catalogue.map((s) => [s.id, s]));
  const labelFor = (id: string) => {
    if (id.startsWith(LOGIN_ROW)) return id.slice(LOGIN_ROW.length);
    const known = byId.get(id)?.label ?? RESOLVER_PROPER_NAMES[id];
    if (known) return known;
    const key = RESOLVER_LABEL_KEYS[id];
    return key ? t(key) : id;
  };

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <Card hue={2} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.routing.priorityHint')}>{t('accounts.routing.priorityTitle')}</SectionTitle>
        {priority === null ? (
          <p className="text-sm text-carbon-textMuted">{t('common.loading')}</p>
        ) : priority.length === 0 ? (
          <p className="text-sm text-carbon-textMuted">{t('accounts.routing.priorityEmpty')}</p>
        ) : (
          <PriorityLadder rows={priority} labelFor={labelFor} jdConfigured={jd?.configured ?? false} onSaved={setPriority} />
        )}
      </Card>

      <Card hue={3} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.routing.jdHint')}>
          {t('accounts.routing.jdTitle')}
        </SectionTitle>
        {jd === null ? (
          <p className="text-sm text-carbon-textMuted">{t('common.loading')}</p>
        ) : !jd.configured ? (
          <p className="text-sm text-carbon-textMuted">{t('accounts.routing.jdNotConfigured')}</p>
        ) : jd.reachable ? (
          <p className="glim-num text-sm text-statusOk">{t('accounts.routing.jdReachable', { version: jd.version ?? 0 })}</p>
        ) : (
          <span className="inline-flex items-center gap-1.5 text-sm text-statusFail">
            {t('accounts.routing.jdUnreachable')}
            {jd.detail && <InfoBubble tip={jd.detail} />}
          </span>
        )}
      </Card>
    </div>
  );
}

/**
 * LadderGrip is the ladder's drag grip. It carries an aria-label but no
 * tooltip, which would follow the pointer down every row.
 */
function LadderGrip({
  label,
  disabled,
  onKeyDown,
  onPointerDown,
}: {
  label: string;
  disabled: boolean;
  onKeyDown: KeyboardEventHandler<HTMLButtonElement>;
  onPointerDown: PointerEventHandler<HTMLButtonElement>;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-label={label}
      className="shrink-0 cursor-grab touch-none rounded-[var(--radius-control)] px-1 py-0.5 text-carbon-textMuted
        outline-none transition-colors hover:text-carbon-text focus-visible:shadow-[0_0_0_2px_var(--focus-ring)]
        active:cursor-grabbing disabled:cursor-default"
      onKeyDown={onKeyDown}
      onPointerDown={onPointerDown}
    >
      <IconGrip width={14} height={16} />
    </button>
  );
}

/**
 * PriorityLadder orders the resolvers by drag or, with the grip focused, by
 * arrow keys. Every drop saves at once and redraws from the server's answer.
 * "Automatisch" clears the stored order, so it follows the ladder as it
 * changes instead of freezing today's.
 */
function PriorityLadder({
  rows,
  labelFor,
  jdConfigured,
  onSaved,
}: {
  rows: ResolverInfo[];
  labelFor: (id: string) => string;
  jdConfigured: boolean;
  onSaved: (rows: ResolverInfo[]) => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  /** The arrangement shown while a drag is in flight; null at rest. */
  const [live, setLive] = useState<string[] | null>(null);
  const [dragId, setDragId] = useState<string | null>(null);
  const list = useRef<HTMLOListElement>(null);

  const order = live ?? rows.map((r) => r.id);
  const byId = new Map(rows.map((r) => [r.id, r] as const));
  const shown = order.map((id) => byId.get(id)).filter((r): r is ResolverInfo => !!r);

  async function store(next: string[]) {
    setBusy(true);
    try {
      onSaved(await saveResolverPriority(next));
    } catch (e) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
    } finally {
      setBusy(false);
      setLive(null);
    }
  }

  /** moved moves `id` to position `to`, clamped, and returns the new order or null. */
  function moved(id: string, to: number): string[] | null {
    const ids = [...order];
    const from = ids.indexOf(id);
    if (from < 0) return null;
    const at = Math.max(0, Math.min(ids.length - 1, to));
    if (at === from) return null;
    ids.splice(from, 1);
    ids.splice(at, 0, id);
    return ids;
  }

  return (
    <div className="flex flex-col gap-3">
      <ol ref={list} className="flex flex-col gap-1.5">
        {shown.map((r, i) => (
          <li
            key={r.id}
            className={`flex items-center gap-2 rounded-[var(--radius-control)] px-1 py-1 text-sm text-carbon-textSub transition-colors ${
              dragId === r.id ? 'bg-carbon-surface2' : ''
            }`}
          >
            {/* Only the grip starts a drag. Pointer events rather than HTML5
                drag, so the list follows every move and works under a finger;
                a real button, so the arrow keys move the row. */}
            <LadderGrip
              label={t('accounts.routing.dragHandle', { name: labelFor(r.id) })}
              disabled={busy}
              onKeyDown={(e) => {
                if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
                e.preventDefault();
                const next = moved(r.id, i + (e.key === 'ArrowUp' ? -1 : 1));
                if (next) void store(next);
              }}
              onPointerDown={(e) => {
                if (busy || e.button !== 0) return;
                e.preventDefault();
                // The row geometry is read before the first move, since the live
                // layout soon shows the preview.
                const items = [...(list.current?.children ?? [])] as HTMLElement[];
                if (items.length < 2) return;
                const first = items[0].getBoundingClientRect();
                const second = items[1].getBoundingClientRect();
                const top = first.top;
                const height = second.top - first.top;
                if (height <= 0) return;

                // Listeners on the document without setPointerCapture, which
                // raised a spurious pointercancel in Chromium (see Tabs.tsx).
                // The arrangement lives here, not in state, so no re-render
                // hands a move a stale copy.
                let arrangement = order;
                setDragId(r.id);
                setLive(order);

                const onMove = (ev: PointerEvent) => {
                  const to = Math.round((ev.clientY - top) / height);
                  const ids = [...arrangement];
                  const from = ids.indexOf(r.id);
                  const at = Math.max(0, Math.min(ids.length - 1, to));
                  if (from < 0 || at === from) return;
                  ids.splice(from, 1);
                  ids.splice(at, 0, r.id);
                  arrangement = ids;
                  setLive(ids);
                };
                const done = () => {
                  document.removeEventListener('pointermove', onMove);
                  document.removeEventListener('pointerup', done);
                  document.removeEventListener('pointercancel', cancel);
                  setDragId(null);
                  // A press without movement is a click, not a reorder.
                  if (arrangement.join() !== rows.map((x) => x.id).join()) void store(arrangement);
                  else setLive(null);
                };
                const cancel = () => {
                  document.removeEventListener('pointermove', onMove);
                  document.removeEventListener('pointerup', done);
                  document.removeEventListener('pointercancel', cancel);
                  setDragId(null);
                  setLive(null);
                };
                document.addEventListener('pointermove', onMove);
                document.addEventListener('pointerup', done);
                document.addEventListener('pointercancel', cancel);
              }}
            />
            <span className="glim-num w-4 shrink-0 text-carbon-textMuted">{i + 1}</span>
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-carbon-text">{labelFor(r.id)}</span>
              {r.id.startsWith(LOGIN_ROW) && (
                <span className="truncate text-[11px] text-carbon-textMuted">{t('accounts.routing.loginRow')}</span>
              )}
            </span>
          </li>
        ))}
      </ol>
      <div className="flex flex-col gap-1.5">
        <span className="glim-eyebrow">{t('accounts.routing.automaticTitle')}</span>
        <ul className="flex flex-col gap-1.5">
          {AUTOMATIC_ROWS.filter((a) => a.id !== 'jd' || jdConfigured).map((a) => (
            <li key={a.id} className="flex items-baseline gap-2 px-1 text-sm">
              <span className="text-carbon-textSub">{labelFor(a.id)}</span>
              <span className="truncate text-[11px] text-carbon-textMuted">{t(a.what)}</span>
            </li>
          ))}
        </ul>
      </div>
      <div className="flex items-center gap-2">
        <Button kind="secondary" disabled={busy} onClick={() => void store([])}>
          {t('accounts.routing.priorityAuto')}
        </Button>
        <InfoBubble tip={t('accounts.routing.priorityAutoHint')} />
      </div>
    </div>
  );
}
