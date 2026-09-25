import { useCallback, useEffect, useState } from 'react';
import {
  type Account,
  type CatalogueService,
  fetchAccountCatalogue,
  fetchAccounts,
  removeAccountCredential,
  saveAccountCredential,
} from '../../lib/api';
import { followExternal } from '../../lib/external';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useToast } from '../../lib/toast';
import { Button, Card, ErrorCard, IconBadge, LoadingCard, PageHeader, SectionTitle, TextInput } from '../../components/ui';
import { IconArrowDown, IconArrowUp, IconExternalLink } from '../../lib/icons';
import { useDraft } from './context';
import { NeutralSwitch } from './controls';
import { ModuleToggle } from './ModuleToggle';

// The captcha page orders the solvers and stores each solver's API key. The
// order lives in the settings draft: an id in captchaSolverOrder is tried in
// that position, an absent one never. Keys go through /api/accounts at once
// and are write-only, because a credential cannot ride the settings document.
// They are saved without a live check.

/**
 * PENDING holds the English strings until the catalogue has them; the lookup
 * asks the catalogue first.
 */
const PENDING = {
  'settings.captcha.title': 'Captcha',
  'settings.captcha.subtitle': 'Automatic solvers are tried in this order before a captcha is ever shown to you.',
  'settings.captcha.orderTitle': 'Solver order',
  'settings.captcha.orderHint':
    'Every enabled solver below is tried in the order shown, top to bottom. If none are enabled, or every one of them fails or declines, you are asked directly.',
  'settings.captcha.orderEmpty': 'No solver is enabled - every captcha comes straight to you.',
  'settings.captcha.use': 'Try this solver',
  'settings.captcha.enableSolver': 'Try {service} automatically',
  'settings.captcha.moveUp': 'Move up',
  'settings.captcha.moveDown': 'Move down',
  'settings.captcha.set': 'Key set',
  'settings.captcha.notSet': 'No key set',
  'settings.captcha.setKey': 'Set key',
  'settings.captcha.change': 'Change',
  'settings.captcha.remove': 'Remove',
  'settings.captcha.cancel': 'Cancel',
  'settings.captcha.save': 'Save',
  'settings.captcha.saving': 'Saving…',
  'settings.captcha.placeholder': 'Paste the API key',
  'settings.captcha.whereToFind': 'Get a key',
  'settings.captcha.saved': 'API key saved.',
  'settings.captcha.removed': 'API key removed.',
  'settings.captcha.saveFailed': 'Could not save the key: {error}',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

export function Captcha() {
  const cx = useCx();
  const { t } = useT();
  const { cfg, patch } = useDraft();

  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [catalogue, setCatalogue] = useState<CatalogueService[]>([]);
  const [loadError, setLoadError] = useState(false);
  const [editing, setEditing] = useState(''); // catalogue id being edited, '' = none

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
  }, [load]);

  if (accounts === null) {
    return loadError ? (
      <ErrorCard message={t('common.loadFailed')} retry={() => void load()} retryLabel={t('common.retry')} />
    ) : (
      <LoadingCard label={t('common.loading')} />
    );
  }

  const order = cfg.captchaSolverOrder ?? [];
  const solvers = catalogue.filter((s) => s.group === 'captchaSolver');
  // Enabled solvers in the chosen order, then the others in catalogue order,
  // so switching one off moves it to the bottom.
  const rows = [
    ...order.map((id) => solvers.find((s) => s.id === id)).filter((s): s is CatalogueService => Boolean(s)),
    ...solvers.filter((s) => !order.includes(s.id)),
  ];

  function setOrder(next: string[]) {
    patch({ captchaSolverOrder: next.length > 0 ? next : null });
  }

  function setEnabled(id: string, on: boolean) {
    if (on) {
      if (!order.includes(id)) setOrder([...order, id]);
    } else {
      setOrder(order.filter((x) => x !== id));
    }
  }

  function move(id: string, by: number) {
    const i = order.indexOf(id);
    const to = i + by;
    if (i < 0 || to < 0 || to >= order.length) return;
    const next = [...order];
    [next[i], next[to]] = [next[to], next[i]];
    setOrder(next);
  }

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={cx('settings.captcha.title')} />

      <Card hue={0} className="flex flex-col gap-1">
        <SectionTitle hint={cx('settings.captcha.orderHint')}>{cx('settings.captcha.orderTitle')}</SectionTitle>
        <ModuleToggle id="captcha" />
        {order.length === 0 && <p className="py-2 text-sm text-carbon-textSub">{cx('settings.captcha.orderEmpty')}</p>}

        <ul className="flex flex-col">
          {rows.map((svc, i) => (
            <SolverRow
              key={svc.id}
              svc={svc}
              hue={i}
              enabled={order.includes(svc.id)}
              position={order.indexOf(svc.id)}
              count={order.length}
              last={i === rows.length - 1}
              account={accounts.find((a) => a.service === svc.id && a.account === '')}
              editing={editing === svc.id}
              onToggle={(on) => setEnabled(svc.id, on)}
              onMove={(by) => move(svc.id, by)}
              onStartEdit={() => setEditing(svc.id)}
              onStopEdit={() => setEditing('')}
              onSaved={load}
            />
          ))}
        </ul>
      </Card>
    </div>
  );
}

function SolverRow({
  svc,
  hue,
  enabled,
  position,
  count,
  last,
  account,
  editing,
  onToggle,
  onMove,
  onStartEdit,
  onStopEdit,
  onSaved,
}: {
  svc: CatalogueService;
  /** Position in the full solver list, for the hue. */
  hue: number;
  enabled: boolean;
  /** Index within the enabled/order list, -1 when not enabled. */
  position: number;
  count: number;
  last: boolean;
  account?: Account;
  editing: boolean;
  onToggle: (on: boolean) => void;
  onMove: (by: number) => void;
  onStartEdit: () => void;
  onStopEdit: () => void;
  onSaved: () => Promise<void>;
}) {
  const cx = useCx();
  const configured = account?.configured ?? false;

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[auto_1fr_auto_auto] items-center gap-3 py-2.5">
        <NeutralSwitch on={enabled} onChange={onToggle} name={cx('settings.captcha.enableSolver', { service: svc.label })} hue={hue} />

        <div className="flex min-w-0 items-center gap-2">
          <span className="text-sm text-carbon-text">{svc.label}</span>
          {svc.whereUrl && (
            <a
              href={svc.whereUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={followExternal}
              className="inline-flex items-center gap-1 text-[11px] text-carbon-textMuted hover:text-carbon-text hover:underline"
            >
              {cx('settings.captcha.whereToFind')}
              <IconExternalLink width={11} height={11} />
            </a>
          )}
        </div>

        <span
          className={`inline-flex shrink-0 items-center gap-1.5 text-[11px] font-medium ${configured ? 'text-statusOk' : 'text-carbon-textMuted'}`}
        >
          <span className={`h-1.5 w-1.5 rounded-[var(--radius-pill)] ${configured ? 'bg-statusOkSolid' : 'bg-carbon-textMuted/50'}`} />
          {configured ? cx('settings.captcha.set') : cx('settings.captcha.notSet')}
        </span>

        {/* `labelled` on every badge, so the actions follow the Beschriftung
            setting like toolbar actions; the name column truncates instead. */}
        <div className="flex shrink-0 items-center gap-0.5">
          {enabled && (
            <>
              <IconBadge
                labelled
                icon={<IconArrowUp width={16} height={16} />}
                hue={hue}
                title={cx('settings.captcha.moveUp')}
                aria-label={cx('settings.captcha.moveUp')}
                disabled={position <= 0}
                onClick={() => onMove(-1)}
              />
              <IconBadge
                labelled
                icon={<IconArrowDown width={16} height={16} />}
                hue={hue}
                title={cx('settings.captcha.moveDown')}
                aria-label={cx('settings.captcha.moveDown')}
                disabled={position < 0 || position >= count - 1}
                onClick={() => onMove(1)}
              />
            </>
          )}
          <Button kind="ghost" onClick={editing ? onStopEdit : onStartEdit}>
            {configured ? cx('settings.captcha.change') : cx('settings.captcha.setKey')}
          </Button>
        </div>
      </div>

      {editing && (
        <div className="pb-3">
          <CredentialEditor
            svc={svc}
            configured={configured}
            onCancel={onStopEdit}
            onSaved={async () => {
              onStopEdit();
              await onSaved();
            }}
          />
        </div>
      )}
    </li>
  );
}

function CredentialEditor({
  svc,
  configured,
  onCancel,
  onSaved,
}: {
  svc: CatalogueService;
  configured: boolean;
  onCancel: () => void;
  onSaved: () => Promise<void>;
}) {
  const cx = useCx();
  const { toast } = useToast();
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);

  async function save() {
    setBusy(true);
    try {
      await saveAccountCredential(svc.id, '', { apiKey: key });
      toast(cx('settings.captcha.saved'), 'ok');
      await onSaved();
    } catch (e) {
      toast(cx('settings.captcha.saveFailed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await removeAccountCredential(svc.id, '');
      toast(cx('settings.captcha.removed'), 'info');
      await onSaved();
    } catch (e) {
      toast(cx('settings.captcha.saveFailed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
      setBusy(false);
    }
  }

  return (
    <div className="flex items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 p-3">
      <div className="min-w-0 flex-1">
        <TextInput
          type="password"
          autoComplete="off"
          autoFocus
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder={cx('settings.captcha.placeholder')}
        />
      </div>
      <Button kind="ghost" onClick={onCancel} disabled={busy}>
        {cx('settings.captcha.cancel')}
      </Button>
      {configured && (
        <Button kind="ghost" onClick={() => void remove()} disabled={busy}>
          {cx('settings.captcha.remove')}
        </Button>
      )}
      <Button onClick={() => void save()} disabled={busy || key.trim() === ''}>
        {busy ? cx('settings.captcha.saving') : cx('settings.captcha.save')}
      </Button>
    </div>
  );
}
