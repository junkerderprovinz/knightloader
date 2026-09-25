// The accounts page: one row per configured service and account, as read from
// internal/accounts/catalogue.go and internal/app/app_accounts.go. Debrid
// accounts and the multihosters reached through JD come first, hoster logins
// below; the section follows the catalogue's Group field, and both cards draw
// an AccountTable.
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
  type CredentialField,
  type HosterHost,
  type HosterLogin,
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
import { LIFT, SETTLE, useReorder } from '../components/dragLift';
import {
  ConfirmRemoveLogin,
  HosterLoginDialog,
  HosterLoginSection,
  hosterLoginRow,
  useHosterLogins,
} from '../components/HosterLoginSection';
import {
  IconAccounts,
  IconChevronStart,
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

/** The form's caption for each named credential field. */
const FIELD_LABELS: Record<CredentialField, TranslationKey> = {
  apiUser: 'accounts.field.apiUser',
  apiKey: 'accounts.field.apiKey',
  customerId: 'accounts.field.customerId',
  email: 'accounts.field.email',
};

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
  const hoster = useHosterLogins(setLoginHosts);
  // A multihoster KnightLoader reaches only through JD: its login dialog, and
  // the login awaiting removal.
  const [jdDialog, setJdDialog] = useState<{ host?: HosterHost; editing?: HosterLogin } | null>(null);
  const [jdConfirming, setJdConfirming] = useState<HosterLogin | null>(null);

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
  const jdLogins = (hoster.logins ?? []).filter((l) => l.multihoster);
  const jdServices = hoster.hosts.filter((h) => h.multihoster && !jdLogins.some((l) => l.host === h.id));
  const jdRows = jdLogins.map((row) =>
    hosterLoginRow(
      row,
      {
        onToggle: (v) => void hoster.toggle(row, v),
        onEdit: () => setJdDialog({ editing: row }),
        onRemove: () => setJdConfirming(row),
      },
      t('accounts.debrid.viaJD'),
    ),
  );

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
        {debridRows.length + jdRows.length > 0 ? (
          <>
            <AccountsTable rows={debridRows} extra={jdRows} {...tableProps} />
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
        <HosterLoginSection data={hoster} />
      </Card>

      {/* The signature, so RoutingSection looks again only when the set of
          services or switched-on logins changes, not on every poll. */}
      <RoutingSection
        catalogue={catalogue}
        signature={`${(accounts ?? []).map((a) => a.service).sort().join(',')}|${loginHosts}`}
      />

      {/* The windows below belong to the debrid card, so they wear its colour. */}
      {dialog && (
        <CredentialDialog
          mode={dialog.mode}
          initial={dialog.mode === 'edit' ? { service: dialog.service, account: dialog.account } : undefined}
          catalogue={catalogue}
          accounts={accounts}
          jdServices={jdServices}
          onPickJD={(host) => {
            setDialog(null);
            setJdDialog({ host });
          }}
          onClose={() => setDialog(null)}
          onSaved={load}
        />
      )}

      {jdDialog && (
        <HosterLoginDialog
          hosts={jdServices}
          existing={hoster.logins ?? []}
          editing={jdDialog.editing}
          initial={jdDialog.host}
          hue={0}
          onClose={() => setJdDialog(null)}
          onSaved={hoster.load}
        />
      )}

      {jdConfirming && (
        <ConfirmRemoveLogin
          login={jdConfirming}
          hue={0}
          onCancel={() => setJdConfirming(null)}
          onConfirm={() => {
            setJdConfirming(null);
            void hoster.remove(jdConfirming.host);
          }}
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
  rows,
  extra,
  catalogue,
  refreshing,
  onRefresh,
  onToggle,
  onRemove,
  onEdit,
}: TableActions & { rows: Account[]; extra: AccountRow[] }) {
  const { t } = useT();
  return (
    <AccountTable
      label={t('accounts.debrid.title')}
      rows={[
        ...rows.map((a): AccountRow => {
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
                      if (svc?.whereUrl) openExternal(svc.whereUrl);
                    },
                  },
                ],
              },
            ],
          };
        }),
        ...extra,
      ]}
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
  jdServices,
  onPickJD,
  onClose,
  onSaved,
}: {
  mode: 'new' | 'edit';
  initial?: { service: string; account: string };
  catalogue: CatalogueService[];
  accounts: Account[];
  /** Multihosters reached through JD, offered beside the services KnightLoader speaks to itself. */
  jdServices: HosterHost[];
  onPickJD: (host: HosterHost) => void;
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

  // Named after the card, its add button and the row's edit action, so every
  // step of the way reads as the same thing.
  const title = !picked
    ? t('accounts.pickDebridAccount')
    : mode === 'edit'
      ? t('accounts.editCredentialTitle', { service: picked.label })
      : t('accounts.addAccountTitle', { service: picked.label });

  return (
    <Modal
      title={title}
      hue={0}
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
          jdServices={jdServices}
          onPickJD={onPickJD}
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
            <Button
              kind="secondary"
              labelled
              icon={<IconChevronStart className="rtl:-scale-x-100" />}
              title={t('accounts.changeAccount')}
              onClick={() => setPicked(null)}
              className="self-start"
            />
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
  jdServices,
  hasDefault,
  onPick,
  onPickJD,
}: {
  services: CatalogueService[];
  jdServices: HosterHost[];
  hasDefault: (id: string) => boolean;
  onPick: (s: CatalogueService) => void;
  onPickJD: (host: HosterHost) => void;
}) {
  const { t } = useT();
  // One alphabetical list: which way a service is reached is a detail of the
  // row, not a reason to look for it in a second place.
  const entries = [
    ...services.map((s) => ({
      key: s.id,
      label: s.label,
      iconHost: s.whereUrl,
      pick: () => onPick(s),
      jd: false,
      connected: hasDefault(s.id),
    })),
    ...jdServices.map((h) => ({
      key: h.id,
      label: h.label,
      iconHost: h.id,
      pick: () => onPickJD(h),
      jd: true,
      connected: false,
    })),
  ].sort((x, y) => x.label.localeCompare(y.label));
  return (
    <div className="flex flex-col gap-3">
      <div className="flex max-h-72 flex-col gap-1 overflow-y-auto">
        {entries.map((e) => (
          <button
            key={e.key}
            type="button"
            onClick={e.pick}
            className="flex items-center gap-3 rounded-[var(--radius-control)] px-3 py-2 text-start hover:bg-carbon-hover"
          >
            <span className="min-w-0 flex-1">
              {/* The service's icon, as in the table. */}
              <span className="flex items-center gap-2 text-sm text-carbon-text">
                <HosterIcon host={e.iconHost} />
                {e.label}
              </span>
            </span>
            {e.jd && <span className="glim-eyebrow shrink-0">{t('accounts.debrid.viaJD')}</span>}
            {e.connected && <span className="glim-eyebrow shrink-0">{t('accounts.connected')}</span>}
          </button>
        ))}
      </div>
    </div>
  );
}

// Routing: the resolver priority order and the JD sidecar's status. Neither is
// an account: the order belongs to the registry (internal/resolver), and the
// sidecar is configured by KL_JD without a credential.

/** The id prefix of a hoster login's row (app.loginRowID). */
const LOGIN_ROW = 'login:';

/**
 * What a row takes, for the rows whose name does not say it. A debrid
 * service's or torrent's row needs no bubble.
 */
const ROW_TIPS: Partial<Record<string, TranslationKey>> = {
  jd: 'accounts.routing.tip.jd',
  ytdlp: 'accounts.routing.tip.ytdlp',
  direct: 'accounts.routing.tip.direct',
};

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
    return byId.get(id)?.label ?? resolverLabel(id, t);
  };

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <Card hue={2} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.routing.orderHint')}>{t('accounts.routing.priorityTitle')}</SectionTitle>
        {priority === null ? (
          <p className="text-sm text-carbon-textMuted">{t('common.loading')}</p>
        ) : priority.length === 0 ? (
          <p className="text-sm text-carbon-textMuted">{t('accounts.routing.priorityEmpty')}</p>
        ) : (
          <PriorityLadder rows={priority} labelFor={labelFor} onSaved={setPriority} />
        )}
      </Card>

      <Card hue={3} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.routing.jdHint')}>
          {t('settings.module.jd')}
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
      className="shrink-0 cursor-grab touch-none rounded-[var(--radius-pill)] px-1 py-0.5 text-carbon-textMuted
        outline-none transition-colors hover:text-carbon-text focus-visible:shadow-[0_0_0_2px_var(--focus-ring)]
        active:cursor-grabbing disabled:cursor-default"
      onKeyDown={onKeyDown}
      onPointerDown={onPointerDown}
    >
      <IconGrip width={14} height={16} />
    </button>
  );
}

/** LadderName is a row's name, with an (i) saying what the row takes where the name does not. */
function LadderName({ id, label }: { id: string; label: string }) {
  const { t } = useT();
  const tip = id.startsWith(LOGIN_ROW) ? 'accounts.routing.tip.login' : ROW_TIPS[id];
  return (
    <span className="flex min-w-0 items-center">
      <span className="truncate text-carbon-text">{label}</span>
      {tip && <InfoBubble tip={t(tip)} />}
    </span>
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
  onSaved,
}: {
  rows: ResolverInfo[];
  labelFor: (id: string) => string;
  onSaved: (rows: ResolverInfo[]) => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  // The order a save is writing, drawn until the server answers, so a dropped
  // row does not jump back to its old place and then forward again.
  const [pending, setPending] = useState<string[] | null>(null);
  const list = useRef<HTMLOListElement>(null);

  async function store(next: string[]) {
    setBusy(true);
    if (next.length > 0) setPending(next);
    try {
      onSaved(await saveResolverPriority(next));
    } catch (e) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
    } finally {
      setBusy(false);
      setPending(null);
    }
  }

  // Only the grip starts a drag, so a mouse arms it by moving; the rows are
  // measured one by one, since a login row is taller than a service row.
  const drag = useReorder({
    ids: pending ?? rows.map((r) => r.id),
    container: list,
    attr: 'data-ladder-id',
    axis: 'y',
    arm: 'move',
    enabled: !busy,
    onReorder: (next) => void store(next),
  });

  const byId = new Map(rows.map((r) => [r.id, r] as const));
  const shown = drag.order.map((id) => byId.get(id)).filter((r): r is ResolverInfo => !!r);

  /** moved moves `id` to position `to`, clamped, and returns the new order or null. */
  function moved(id: string, to: number): string[] | null {
    const ids = [...drag.order];
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
      {/* The list is the rows' offsetParent, the layout a drag measures in. */}
      <ol ref={list} className="relative flex flex-col gap-1.5">
        {shown.map((r, i) => {
          const look = drag.look(r.id);
          const carried = look === LIFT || look === SETTLE;
          const wiggling = drag.held !== null && drag.held !== r.id;
          return (
            <li
              key={r.id}
              data-ladder-id={r.id}
              // A carried row floats over the others, so it takes a ground of
              // its own. select-none keeps a drag from selecting the names.
              className={`flex select-none items-center gap-2 rounded-[var(--radius-control)] px-1 py-1 text-sm
                text-carbon-textSub ${carried ? 'bg-carbon-surface2' : ''} ${wiggling ? 'glim-tab-wiggle' : ''} ${look}`}
            >
              {/* A real button, so the arrow keys move the row. */}
              <LadderGrip
                label={t('accounts.routing.dragHandle', { name: labelFor(r.id) })}
                disabled={busy}
                onKeyDown={(e) => {
                  if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
                  e.preventDefault();
                  const next = moved(r.id, i + (e.key === 'ArrowUp' ? -1 : 1));
                  if (next) void store(next);
                }}
                onPointerDown={(e) => drag.press(e, r.id)}
              />
              <span className="glim-num w-4 shrink-0 text-carbon-textMuted">{i + 1}</span>
              <LadderName id={r.id} label={labelFor(r.id)} />
            </li>
          );
        })}
      </ol>
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
    </div>
  );
}
