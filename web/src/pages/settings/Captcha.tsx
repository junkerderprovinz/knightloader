import { useCallback, useEffect, useState } from 'react';
import {
  type Account,
  type CatalogueService,
  fetchAccountCatalogue,
  fetchAccounts,
  removeAccountCredential,
  saveAccountCredential,
} from '../../lib/api';
import { useT } from '../../lib/i18n';
import { useToast } from '../../lib/toast';
import {
  Button,
  Card,
  ErrorCard,
  Field,
  IconBadge,
  LinkBadge,
  LoadingCard,
  NumberInput,
  PageHeader,
  SectionTitle,
  TextInput,
  ToggleRow,
} from '../../components/ui';
import { IconArrowDown, IconArrowUp } from '../../lib/icons';
import { useDraft } from './context';
import { NeutralSwitch } from './controls';
import { ModuleToggle } from './ModuleToggle';

// The captcha page orders the solvers, stores each solver's API key and says
// whether the solvers wait for somebody watching. The order lives in the
// settings draft: an id in captchaSolverOrder is tried in that position, an
// absent one never. Keys go through /api/accounts at once and are write-only,
// because a credential cannot ride the settings document. They are saved
// without a live check.

// Copies of the bounds in internal/settings/settings_captcha.go, which no route
// serves.
const MIN_WAIT = 10;
const MAX_WAIT = 600;

export function Captcha() {
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
      <PageHeader title={t('settings.captcha.title')} />

      <Card hue={0} className="flex flex-col gap-1">
        <SectionTitle hint={t('settings.captcha.orderHint')}>{t('settings.captcha.orderTitle')}</SectionTitle>
        <ModuleToggle id="captcha" />
        {order.length === 0 && <p className="py-2 text-sm text-carbon-textSub">{t('settings.captcha.orderEmpty')}</p>}

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

      {/* Absent while no solver is enabled, since nothing reads these then. */}
      {order.length > 0 && (
        <Card hue={1} className="flex flex-col gap-5">
          <SectionTitle>{t('settings.captcha.whenTitle')}</SectionTitle>
          <ToggleRow
            hue={0}
            checked={cfg.captchaSolverOnlyUnwatched}
            onChange={(v) => patch({ captchaSolverOnlyUnwatched: v })}
            label={t('settings.captcha.onlyUnwatched')}
            hint={t('settings.captcha.onlyUnwatchedHint')}
          />
          {/* sanitizeCaptcha clamps to 10..600; onValue does not, or 60 could
              not be typed digit by digit. */}
          {cfg.captchaSolverOnlyUnwatched && (
            <Field label={t('settings.captcha.wait')} hint={t('settings.captcha.waitHint')}>
              <NumberInput
                value={cfg.captchaSolverWait}
                min={MIN_WAIT}
                max={MAX_WAIT}
                step={10}
                onValue={(v) => patch({ captchaSolverWait: v })}
              />
            </Field>
          )}
        </Card>
      )}
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
  const { t } = useT();
  const configured = account?.configured ?? false;

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[auto_1fr_auto_auto] items-center gap-3 py-2.5">
        <NeutralSwitch on={enabled} onChange={onToggle} name={t('settings.captcha.enableSolver', { service: svc.label })} hue={hue} />

        <div className="flex min-w-0 items-center gap-2">
          <span className="text-sm text-carbon-text">{svc.label}</span>
          {svc.whereUrl && <LinkBadge href={svc.whereUrl} title={t('accounts.whereToFind')} />}
        </div>

        <span
          className={`inline-flex shrink-0 items-center gap-1.5 text-[11px] font-medium ${configured ? 'text-statusOk' : 'text-carbon-textMuted'}`}
        >
          <span className={`h-1.5 w-1.5 rounded-[var(--radius-pill)] ${configured ? 'bg-statusOkSolid' : 'bg-carbon-textMuted/50'}`} />
          {configured ? t('settings.captcha.set') : t('settings.captcha.notSet')}
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
                title={t('settings.captcha.moveUp')}
                aria-label={t('settings.captcha.moveUp')}
                disabled={position <= 0}
                onClick={() => onMove(-1)}
              />
              <IconBadge
                labelled
                icon={<IconArrowDown width={16} height={16} />}
                hue={hue}
                title={t('settings.captcha.moveDown')}
                aria-label={t('settings.captcha.moveDown')}
                disabled={position < 0 || position >= count - 1}
                onClick={() => onMove(1)}
              />
            </>
          )}
          <Button kind="ghost" onClick={editing ? onStopEdit : onStartEdit}>
            {configured ? t('settings.captcha.change') : t('settings.captcha.setKey')}
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
  const { t } = useT();
  const { toast } = useToast();
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);

  async function save() {
    setBusy(true);
    try {
      await saveAccountCredential(svc.id, '', { apiKey: key });
      toast(t('settings.captcha.saved'), 'ok');
      await onSaved();
    } catch (e) {
      toast(t('settings.captcha.saveFailed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await removeAccountCredential(svc.id, '');
      toast(t('settings.captcha.removed'), 'info');
      await onSaved();
    } catch (e) {
      toast(t('settings.captcha.saveFailed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
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
          placeholder={t('settings.captcha.placeholder')}
        />
      </div>
      <Button kind="ghost" onClick={onCancel} disabled={busy}>
        {t('settings.captcha.cancel')}
      </Button>
      {configured && (
        <Button kind="ghost" onClick={() => void remove()} disabled={busy}>
          {t('settings.captcha.remove')}
        </Button>
      )}
      <Button onClick={() => void save()} disabled={busy || key.trim() === ''}>
        {busy ? t('settings.captcha.saving') : t('settings.captcha.save')}
      </Button>
    </div>
  );
}
