// The account entity, page and all: one row per configured (service, account)
// pair, never one per catalogue entry - see internal/accounts/catalogue.go and
// internal/app/app_accounts.go, the sources this page reads from rather than
// deciding on its own.
//
// Two sections, by direct instruction: Debrid on top (the convenient path -
// one key covers many hosters), Hoster logins below (individual per-hoster
// accounts). Which section a service belongs to comes from the catalogue's
// own Group field, never a hardcoded id list here - the same reason
// AccountsTable is one component the two sections both call, filtered by
// group at the call site instead of by an if/else on ids baked into it.
import { useCallback, useEffect, useState } from 'react';
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
  setAccountEnabled,
  setAccountLabel,
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
  IconBadge,
  InfoBubble,
  LoadingCard,
  Modal,
  PageHeader,
  SectionTitle,
  TextInput,
  Toggle,
} from '../components/ui';
import { ContextMenu, anchorBelow, useContextMenu } from '../components/ContextMenu';
import { HosterLoginSection } from '../components/HosterLoginSection';
import { IconAccounts, IconEdit, IconExternalLink, IconPlus, IconRetry, IconSearch, IconSettings, IconTrash } from '../lib/icons';

// Passive poll for whatever the account-health refresher (agent 6B) writes in
// the background - this page never runs that check itself, only reads its
// result. Slow on purpose: expiry and traffic figures move in hours, not
// seconds, and a tighter interval would only hammer the same stored answer.
const HEALTH_POLL_MS = 30000;

type DialogState = { mode: 'new' } | { mode: 'edit'; service: string; account: string };

export function Accounts() {
  const { t } = useT();
  const { toast } = useToast();
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [catalogue, setCatalogue] = useState<CatalogueService[]>([]);
  const [loadError, setLoadError] = useState(false);
  const [dialog, setDialog] = useState<DialogState | null>(null);
  const [refreshing, setRefreshing] = useState<ReadonlySet<string>>(new Set());

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
    // Optimistic: the switch is the row's only feedback, and a spinner over a
    // toggle reads as broken rather than as busy.
    setAccounts((cur) => cur?.map((x) => (x.id === a.id ? { ...x, enabled } : x)) ?? cur);
    try {
      await setAccountEnabled(a.service, a.account, enabled);
    } catch {
      toast(t('common.loadFailed'), 'fail');
      await load();
    }
  }

  async function onRename(a: Account, label: string) {
    try {
      await setAccountLabel(a.service, a.account, label);
      setAccounts((cur) => cur?.map((x) => (x.id === a.id ? { ...x, label } : x)) ?? cur);
    } catch {
      toast(t('common.loadFailed'), 'fail');
    }
  }

  async function onRemove(a: Account) {
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
  // Hoster accounts never come from the catalogue - see HosterLoginSection
  // below, which owns internal/hosterauth's own host-keyed list. A catalogue
  // entry is a fixed, known service (TorBox and its like); a hoster login is
  // any of the ones JD already knows, picked ad hoc, so it was never going to
  // fit the same "one row per catalogue id" shape debrid accounts use.
  const debridIds = new Set(catalogue.filter((s) => s.group === 'debrid').map((s) => s.id));
  const debridRows = accounts.filter((a) => debridIds.has(a.service));

  const tableProps = { catalogue: byId, refreshing, onRefresh, onToggle, onRename, onRemove, onEdit };

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('accounts.title')} />

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={0} hint={t('accounts.debrid.hint')}>
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

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={1} hint={t('accounts.hoster.hint')}>
          {t('accounts.hoster.title')}
        </SectionTitle>
        <HosterLoginSection />
      </Card>

      <RoutingSection catalogue={catalogue} />

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
    </div>
  );
}

// ---- the table -------------------------------------------------------------

interface TableActions {
  catalogue: Map<string, CatalogueService>;
  refreshing: ReadonlySet<string>;
  onRefresh: (a: Account) => void;
  onToggle: (a: Account, enabled: boolean) => void;
  onRename: (a: Account, label: string) => void;
  onRemove: (a: Account) => void;
  onEdit: (a: Account) => void;
}

function AccountsTable({ rows, catalogue, refreshing, onRefresh, onToggle, onRename, onRemove, onEdit }: TableActions & { rows: Account[] }) {
  const { t } = useT();
  const menu = useContextMenu();
  const [menuRow, setMenuRow] = useState<Account | null>(null);
  const [renaming, setRenaming] = useState<{ id: string; value: string } | null>(null);

  function commitRename() {
    if (!renaming) return;
    const row = rows.find((r) => r.id === renaming.id);
    const value = renaming.value.trim();
    setRenaming(null);
    if (row && value && value !== row.label) onRename(row, value);
  }

  return (
    <div className="glim-well overflow-x-auto p-0">
      <table className="w-full min-w-[42rem] border-collapse text-sm">
        <thead>
          <tr className="text-start text-xs text-carbon-textMuted">
            <th className="w-12 px-4 py-3 text-start font-medium">{t('accounts.col.enabled')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.service')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.status')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.label')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.expiry')}</th>
            <th className="px-2 py-3 text-start font-medium">{t('accounts.col.traffic')}</th>
            <th className="w-10 px-2 py-3">
              <span className="sr-only">{t('accounts.rowActions')}</span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-carbon-border/40">
          {rows.map((a, i) => {
            const svc = catalogue.get(a.service);
            const busy = refreshing.has(a.id);
            return (
              <tr key={a.id} className="group transition-colors hover:bg-carbon-hover">
                <td className="px-4 py-3">
                  <Toggle
                    checked={a.enabled}
                    onChange={(v) => onToggle(a, v)}
                    label={t('accounts.enableAccount', {
                      account: a.label ? `${svc?.label ?? a.service} — ${a.label}` : (svc?.label ?? a.service),
                    })}
                    hideLabel
                  />
                </td>
                <td className="px-2 py-3 font-medium text-carbon-text">
                  <span className="inline-flex items-center gap-1">
                    {svc?.label ?? a.service}
                    {a.hostsFetchedAt && <InfoBubble tip={t('accounts.hostsRefreshed', { when: fmtDate(a.hostsFetchedAt) })} />}
                  </span>
                </td>
                <td className="px-2 py-3">
                  <AccountStatus account={a} busy={busy} />
                </td>
                <td className="px-2 py-3">
                  {renaming?.id === a.id ? (
                    <input
                      autoFocus
                      value={renaming.value}
                      onChange={(e) => setRenaming({ id: a.id, value: e.target.value })}
                      onBlur={commitRename}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') commitRename();
                        if (e.key === 'Escape') setRenaming(null);
                      }}
                      className="w-full min-w-[8rem] rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-1
                        text-sm text-carbon-text outline-none focus:shadow-[0_0_0_2px_var(--focus-ring)]"
                    />
                  ) : (
                    <button
                      type="button"
                      onClick={() => setRenaming({ id: a.id, value: a.label })}
                      title={t('accounts.rename')}
                      aria-label={a.label ? undefined : t('accounts.rename')}
                      className="rounded-[var(--radius-control)] px-1 py-0.5 text-start text-carbon-textSub
                        hover:bg-carbon-surface2 hover:text-carbon-text"
                    >
                      {a.label || '—'}
                    </button>
                  )}
                </td>
                <td className="glim-num px-2 py-3 text-carbon-textSub">{a.expiry || '—'}</td>
                <td className="glim-num px-2 py-3 text-carbon-textSub">{a.trafficLeft || '—'}</td>
                <td className="px-2 py-3 text-end">
                  <IconBadge
                    hue={i}
                    className="opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100"
                    icon={<IconSettings width={16} height={16} />}
                    title={t('accounts.rowActions')}
                    aria-label={t('accounts.rowActions')}
                    onClick={(e) => {
                      setMenuRow(a);
                      menu.openAt(anchorBelow(e.currentTarget));
                    }}
                  />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>

      {menu.anchor && menuRow && (
        <ContextMenu
          anchor={menu.anchor}
          label={t('accounts.rowActions')}
          onClose={menu.close}
          groups={[
            {
              id: 'actions',
              items: [
                {
                  id: 'refresh',
                  label: t('accounts.refresh'),
                  icon: <IconRetry width={16} height={16} />,
                  onSelect: () => onRefresh(menuRow),
                },
                {
                  id: 'edit',
                  label: t('accounts.edit'),
                  icon: <IconEdit width={16} height={16} />,
                  onSelect: () => onEdit(menuRow),
                },
                {
                  id: 'renew',
                  label: menuRow.expiry ? t('accounts.renew') : t('accounts.buyPremium'),
                  icon: <IconExternalLink width={16} height={16} />,
                  // Only ever actionable once there is something to renew - a
                  // link with nowhere useful to send someone must not pretend
                  // to be live.
                  disabled: !menuRow.expiry || !catalogue.get(menuRow.service)?.whereUrl,
                  onSelect: () => {
                    const url = catalogue.get(menuRow.service)?.whereUrl;
                    if (url) window.open(url, '_blank', 'noopener,noreferrer');
                  },
                },
              ],
            },
            {
              id: 'danger',
              // A credential the container supplies cannot be cleared from
              // here - there is nothing in the encrypted store to remove, and
              // an action that looks like it deletes the account but leaves
              // it right back on the next reload is worse than no action.
              items: menuRow.fromEnv
                ? []
                : [
                    {
                      id: 'remove',
                      label: t('accounts.remove'),
                      icon: <IconTrash width={16} height={16} />,
                      danger: true,
                      onSelect: () => onRemove(menuRow),
                    },
                  ],
            },
          ]}
        />
      )}
    </div>
  );
}

function AccountStatus({ account, busy }: { account: Account; busy: boolean }) {
  const { t } = useT();
  if (busy) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] font-medium text-carbon-textMuted">
        <span className="inline-flex animate-spin">
          <IconRetry width={13} height={13} />
        </span>
        {t('accounts.refreshing')}
      </span>
    );
  }
  // Nothing has ever checked this row yet - not the same as a check that came
  // back negative, so it gets its own neutral reading rather than borrowing
  // "failed".
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

// ---- new/edit credential dialog --------------------------------------------

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

  const [query, setQuery] = useState('');
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
  const filtered = catalogue.filter((s) => s.label.toLowerCase().includes(query.trim().toLowerCase()));

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
        // Verified failures stop here: "save anyway" is a second, deliberate
        // click, never a fallback this function takes on its own - an offline
        // service must not block the save, but it must not be silently
        // skipped past either.
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
            <Button kind="ghost" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            {verifyResult && !verifyResult.ok && (
              <Button kind="secondary" onClick={() => void doSave(true)} disabled={saving}>
                {t('accounts.saveAnyway')}
              </Button>
            )}
            <Button onClick={() => void doSave(false)} disabled={saving || !credentialFilled() || accountIdRequired}>
              {verifying ? t('accounts.verifying') : saving ? t('accounts.saving') : t('accounts.save')}
            </Button>
          </>
        ) : picked ? (
          <>
            <span className="flex-1" />
            <Button kind="secondary" onClick={onClose}>
              {t('common.cancel')}
            </Button>
          </>
        ) : undefined
      }
    >
      {!picked ? (
        <ServicePicker
          query={query}
          onQuery={setQuery}
          services={filtered}
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

              {picked.kind === 'apiKey' ? (
                <Field label={t('accounts.keyLabel', { service: picked.label })} hint={t('accounts.keyHint')}>
                  <TextInput
                    type="password"
                    autoComplete="off"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                    placeholder={t('accounts.placeholder')}
                  />
                </Field>
              ) : (
                <>
                  <Field label={t('accounts.usernameField')}>
                    <TextInput autoComplete="off" value={username} onChange={(e) => setUsername(e.target.value)} />
                  </Field>
                  <Field label={t('accounts.passwordField')}>
                    <TextInput type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
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
  query,
  onQuery,
  services,
  hasDefault,
  onPick,
}: {
  query: string;
  onQuery: (q: string) => void;
  services: CatalogueService[];
  hasDefault: (id: string) => boolean;
  onPick: (s: CatalogueService) => void;
}) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
        <IconSearch width={15} height={15} className="shrink-0 text-carbon-textMuted" />
        <input
          autoFocus
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder={t('accounts.searchServices')}
          aria-label={t('accounts.searchServices')}
          className="min-w-0 flex-1 bg-transparent text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none"
        />
      </div>
      <div className="flex max-h-72 flex-col gap-1 overflow-y-auto">
        {services.length === 0 && <p className="px-2 py-3 text-center text-sm text-carbon-textMuted">{t('accounts.noServicesFound')}</p>}
        {services.map((s) => (
          <button
            key={s.id}
            type="button"
            onClick={() => onPick(s)}
            className="flex items-center gap-3 rounded-[var(--radius-control)] px-3 py-2 text-start hover:bg-carbon-hover"
          >
            <span className="min-w-0 flex-1">
              <span className="block text-sm text-carbon-text">{s.label}</span>
              <span className="block text-[11px] text-carbon-textMuted">
                {s.group === 'debrid' ? t('accounts.debrid.title') : t('accounts.hoster.title')}
              </span>
            </span>
            {hasDefault(s.id) && <span className="glim-eyebrow shrink-0">{t('accounts.connected')}</span>}
          </button>
        ))}
      </div>
    </div>
  );
}

// ---- routing: priority order + the JD sidecar's own status ----------------
//
// Neither of these is an account, which is why it is a section of its own
// rather than a row in AccountsTable: the priority order is a fact about the
// REGISTRY (internal/resolver), not about any one credential, and the JD
// sidecar is configured by a URL (KL_JD) with no catalogue entry at all - it
// cannot become an AccountsTable row without a credential the container
// never gives it.

/** RESOLVER_LABEL_KEYS names the locale key for a resolver whose label is a
 *  genuinely descriptive phrase, worth translating - "direct" and "http" are
 *  what KL calls its own built-in fetch paths, not a product name. Typed by
 *  TranslationKey rather than plain string so a lookup through it stays a key
 *  t() actually accepts, not a widened string tsc can no longer check. */
const RESOLVER_LABEL_KEYS: Partial<Record<string, TranslationKey>> = {
  direct: 'accounts.routing.resolver.direct',
  http: 'accounts.routing.resolver.http',
};

/** RESOLVER_PROPER_NAMES is the other half: a resolver whose label is a
 *  project/product name - yt-dlp, JDownloader - the same reason
 *  torbox/alldebrid/realdebrid read their label off the catalogue instead of
 *  a locale key. Deliberately not run through t(): a proper noun does not
 *  change across the 38 locales this app ships, only the word around it does. */
const RESOLVER_PROPER_NAMES: Record<string, string> = {
  ytdlp: 'yt-dlp',
  jd: 'JDownloader',
};

function RoutingSection({ catalogue }: { catalogue: CatalogueService[] }) {
  const { t } = useT();
  const [priority, setPriority] = useState<ResolverInfo[] | null>(null);
  const [jd, setJd] = useState<JDStatus | null>(null);

  useEffect(() => {
    let live = true;
    void fetchResolverPriority().then((p) => live && setPriority(p));
    void fetchJDStatus().then((s) => live && setJd(s));
    return () => {
      live = false;
    };
  }, []);

  const byId = new Map(catalogue.map((s) => [s.id, s]));
  const labelFor = (id: string) => {
    const known = byId.get(id)?.label ?? RESOLVER_PROPER_NAMES[id];
    if (known) return known;
    const key = RESOLVER_LABEL_KEYS[id];
    return key ? t(key) : id;
  };

  return (
    // No outer "Weiterleitung" title any more - jdp, 2026-08-23: "badge ist
    // immer noch da, jetzt nur weiter unten unter dem
    // Prioritätsreihenfolge-badge. bitte entfernen." The two cards below
    // each already carry their own clear title, so the umbrella label over
    // both was redundant and, sitting this close above the grid, crowded
    // the Prioritätsreihenfolge card's own badge instead of reading as a
    // section header. That removed title used to own hue 2, which left this
    // page's SectionTitle sequence at 0 (debrid), 1 (hoster), 3, 4 - a gap
    // that read as an arbitrary, non-sequential rainbow once the debrid/
    // hoster cards above got their own real .glim-card box and all four
    // badges became visible together as one set for the first time (jdp,
    // 2026-08-24: "jetzt sind die card falsch eingefärbt"). Renumbered to 2
    // and 3 below so the whole page runs 0-1-2-3 with no skip, matching
    // every other multi-card settings page (Look.tsx, Access.tsx).
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <Card className="flex flex-col gap-3">
        <SectionTitle hue={2} hint={t('accounts.routing.priorityHint')}>{t('accounts.routing.priorityTitle')}</SectionTitle>
        {priority === null ? (
          <p className="text-sm text-carbon-textMuted">{t('common.loading')}</p>
        ) : priority.length === 0 ? (
          <p className="text-sm text-carbon-textMuted">{t('accounts.routing.priorityEmpty')}</p>
        ) : (
          <ol className="flex flex-col gap-1.5">
            {priority.map((r, i) => (
              <li key={r.id} className="flex items-center gap-2 text-sm text-carbon-textSub">
                <span className="glim-num w-4 shrink-0 text-carbon-textMuted">{i + 1}</span>
                <span className="text-carbon-text">{labelFor(r.id)}</span>
              </li>
            ))}
          </ol>
        )}
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={3} hint={t('accounts.routing.jdHint')}>
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
