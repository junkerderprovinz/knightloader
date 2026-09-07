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
  IconBadge,
  InfoBubble,
  LoadingCard,
  Modal,
  PageHeader,
  SectionTitle,
  TextInput,
  Toggle,
} from '../components/ui';
import { AccountTable } from '../components/AccountTable';
import { HosterLoginSection } from '../components/HosterLoginSection';
import {
  IconAccounts,
  IconArrowDown,
  IconArrowUp,
  IconEdit,
  IconExternalLink,
  IconPlus,
  IconRetry,
  IconSearch,
  IconSettings,
  IconTrash,
} from '../lib/icons';
import { HosterIcon } from '../components/HosterIcon';

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

  const tableProps = { catalogue: byId, refreshing, onRefresh, onToggle, onRemove, onEdit };

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
          // The service's own icon, taken from the site its "where do I get a
          // key" link already points at - the one host string the catalogue
          // carries for a debrid service.
          iconHost: svc?.whereUrl ?? '',
          label: svc?.label ?? a.service,
          enabled: a.enabled,
          status: <AccountStatus account={a} busy={refreshing.has(a.id)} />,
          tier: a.tier,
          expiry: a.expiry,
          traffic: a.traffic,
          onToggle: (v) => onToggle(a, v),
          onEdit: () => onEdit(a),
          // A credential the container supplies cannot be cleared from here -
          // there is nothing in the encrypted store to remove, and an action
          // that looks like it deletes the account but leaves it right back on
          // the next reload is worse than no action.
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
                  // Only ever actionable once there is something to renew - a
                  // link with nowhere useful to send someone must not pretend
                  // to be live.
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
  // Debrid only. The catalogue also carries the captcha solvers (see
  // accounts.Group's own doc comment), and this page renders exactly one table
  // of debrid rows - so offering 2Captcha here let somebody save a key that
  // then appeared nowhere on the page they saved it from. Those two are
  // configured on the Captcha settings page, beside the switches they belong
  // to.
  const filtered = catalogue.filter(
    (s) => s.group === 'debrid' && s.label.toLowerCase().includes(query.trim().toLowerCase()),
  );

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
              {/* The service's own icon here too, so the picker and the table
                  it fills read as the same list (jdp, 2026-09-06: "Die
                  debridaccount haben kein logo in der liste"). */}
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
  // Was missing, and the ladder printed the bare id "torrent" for it - the
  // one row in the list that read like a bug rather than a service.
  torrent: 'accounts.routing.resolver.torrent',
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
      <Card hue={2} className="flex flex-col gap-3">
        <SectionTitle hint={t('accounts.routing.priorityHint')}>{t('accounts.routing.priorityTitle')}</SectionTitle>
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
 * The priority ladder, hand-arrangeable (jdp, 2026-09-07: "Die
 * Prioritätsreihenfolge soll per drag and drop anordenbar sein").
 *
 * Two ways to move a row, on purpose. Drag is the one that was asked for and
 * the one that feels right for a short list; the two arrow badges beside each
 * row are the same move without a pointer, which is what makes this reachable
 * from a keyboard and on a touch screen, where an HTML5 drag does not fire at
 * all. They are not a fallback bolted on - they run the identical `move`.
 *
 * Every drop saves immediately and redraws from the SERVER's answer rather
 * than from the local array: what is stored is what the downloader will walk,
 * and a list that kept showing the arrangement the drop produced would hide a
 * rejected or de-duplicated entry.
 *
 * "Automatisch" clears the stored order rather than writing the current one
 * out. Those are genuinely different: an empty order follows the ladder as it
 * changes (a new debrid key, a login going premium), a written-out copy of
 * today's order freezes it.
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
  const [dragId, setDragId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function store(order: string[]) {
    setBusy(true);
    try {
      onSaved(await saveResolverPriority(order));
    } catch (e) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
    } finally {
      setBusy(false);
    }
  }

  /** Moves `id` to the position `to`, clamped, and stores the result. */
  function move(id: string, to: number) {
    const ids = rows.map((r) => r.id);
    const from = ids.indexOf(id);
    if (from < 0) return;
    const at = Math.max(0, Math.min(ids.length - 1, to));
    if (at === from) return;
    ids.splice(from, 1);
    ids.splice(at, 0, id);
    void store(ids);
  }

  return (
    <div className="flex flex-col gap-3">
      <ol className="flex flex-col gap-1.5">
        {rows.map((r, i) => (
          <li
            key={r.id}
            draggable={!busy}
            onDragStart={(e) => {
              setDragId(r.id);
              e.dataTransfer.effectAllowed = 'move';
              // Firefox refuses to start a drag at all without payload.
              e.dataTransfer.setData('text/plain', r.id);
            }}
            onDragEnd={() => setDragId(null)}
            // preventDefault on dragover, not only on drop: without it the
            // browser's own default for an unhandled dragover refuses the
            // drop outright and nothing ever fires.
            onDragOver={(e) => dragId && e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault();
              if (dragId && dragId !== r.id) move(dragId, i);
              setDragId(null);
            }}
            className={`flex items-center gap-2 rounded-[var(--radius-control)] px-1 py-0.5 text-sm text-carbon-textSub ${
              dragId === r.id ? 'opacity-40' : ''
            } ${busy ? '' : 'cursor-grab active:cursor-grabbing'}`}
          >
            <span className="glim-num w-4 shrink-0 text-carbon-textMuted">{i + 1}</span>
            <span className="text-carbon-text">{labelFor(r.id)}</span>
            <span className="flex-1" />
            <IconBadge
              icon={<IconArrowUp width={14} height={14} />}
              className="h-6 w-6"
              title={t('accounts.routing.moveUp')}
              aria-label={t('accounts.routing.moveUp')}
              disabled={busy || i === 0}
              onClick={() => move(r.id, i - 1)}
            />
            <IconBadge
              icon={<IconArrowDown width={14} height={14} />}
              className="h-6 w-6"
              title={t('accounts.routing.moveDown')}
              aria-label={t('accounts.routing.moveDown')}
              disabled={busy || i === rows.length - 1}
              onClick={() => move(r.id, i + 1)}
            />
          </li>
        ))}
      </ol>
      <div className="flex items-center gap-2">
        <Button kind="secondary" disabled={busy} onClick={() => void store([])}>
          {t('accounts.routing.priorityAuto')}
        </Button>
        <InfoBubble tip={t('accounts.routing.priorityAutoHint')} />
      </div>
    </div>
  );
}
