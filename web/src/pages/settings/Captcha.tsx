import { useCallback, useEffect, useState } from 'react';
import {
  type Account,
  ApiError,
  type CatalogueService,
  createTestCaptcha,
  fetchAccountCatalogue,
  fetchAccounts,
} from '../../lib/api';
import { type TranslationKey, useT } from '../../lib/i18n';
import { useToast } from '../../lib/toast';
import {
  Button,
  Card,
  ErrorCard,
  Field,
  IconBadge,
  InfoBubble,
  LinkBadge,
  LoadingCard,
  NumberInput,
  PageHeader,
  SectionTitle,
  ToggleRow,
} from '../../components/ui';
import { IconArrowDown, IconArrowUp } from '../../lib/icons';
import { useDraft } from './context';
import { NeutralSwitch } from './controls';
import { ModuleToggle, PageBadge } from './ModuleToggle';

// The captcha page orders the solvers and says whether they wait for somebody
// watching. The order lives in the settings draft: an id in captchaSolverOrder
// is tried in that position, an absent one never. The solvers' keys are
// accounts and are kept on the Accounts page; this page only reads whether one
// is set.

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
              state={keyState(svc.id, accounts)}
              onToggle={(on) => setEnabled(svc.id, on)}
              onMove={(by) => move(svc.id, by)}
            />
          ))}
        </ul>
        <div className="pt-2">
          <PageBadge page="accounts" title={t('settings.captcha.keys')} />
        </div>
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

      <TestCaptchaCard hue={order.length > 0 ? 2 : 1} solvers={canTakeATest(order, accounts)} />
    </div>
  );
}

/**
 * canTakeATest reports whether one of the solvers in order would get a test
 * captcha: it needs a key and must not be switched off on the Accounts page,
 * as the instance's captchaSolvers decides.
 */
export function canTakeATest(order: string[], accounts: Account[]): boolean {
  return order.some((id) => keyState(id, accounts) === 'set');
}

type KeyState = 'set' | 'off' | 'notSet';

/**
 * keyState says how the solver's account stands on the Accounts page. A key
 * on a switched-off account counts as off, since the instance hands that
 * account nothing.
 */
export function keyState(id: string, accounts: Account[]): KeyState {
  const a = accounts.find((x) => x.service === id && x.account === '' && x.configured);
  if (!a) return 'notSet';
  return a.enabled ? 'set' : 'off';
}

const TEST_REFUSALS: Partial<Record<string, TranslationKey>> = {
  captchaOff: 'settings.captcha.testOff',
  captchaJDOff: 'settings.captcha.testJDOff',
  noCaptchaAccount: 'settings.captcha.testNoAccount',
};

/** testRefusal is the text that says why a test captcha did not go up. */
export function testRefusal(e: unknown): TranslationKey {
  return (e instanceof ApiError && TEST_REFUSALS[e.code ?? '']) || 'captcha.networkError';
}

/**
 * Puts up a test captcha, which arrives in the captcha window and the phone app
 * like a real one; how the answer compared comes back as a toast from
 * CaptchaModal. The captcha accounts bill a test like any captcha, so it goes
 * to them only from the second button, which shows while one of them could
 * take it.
 */
function TestCaptchaCard({ hue, solvers }: { hue: number; solvers: boolean }) {
  const { t } = useT();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);

  async function send(toSolvers: boolean) {
    setBusy(true);
    try {
      await createTestCaptcha(toSolvers);
    } catch (e) {
      toast(t(testRefusal(e)), 'fail');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle>{t('settings.captcha.testTitle')}</SectionTitle>
      <div className="flex flex-wrap gap-2">
        <Button kind="secondary" hue={0} disabled={busy} hint={t('settings.captcha.testHint')} onClick={() => void send(false)}>
          {t('settings.captcha.test')}
        </Button>
        {solvers && (
          <Button
            kind="secondary"
            hue={1}
            disabled={busy}
            hint={t('settings.captcha.testSolversHint')}
            onClick={() => void send(true)}
          >
            {t('settings.captcha.testSolvers')}
          </Button>
        )}
      </div>
    </Card>
  );
}

const KEY_STATE = {
  set: 'settings.captcha.set',
  off: 'settings.captcha.off',
  notSet: 'settings.captcha.notSet',
} as const satisfies Record<KeyState, TranslationKey>;

export function SolverRow({
  svc,
  hue,
  enabled,
  position,
  count,
  last,
  state,
  onToggle,
  onMove,
}: {
  svc: CatalogueService;
  /** Position in the full solver list, for the hue. */
  hue: number;
  enabled: boolean;
  /** Index within the enabled/order list, -1 when not enabled. */
  position: number;
  count: number;
  last: boolean;
  state: KeyState;
  onToggle: (on: boolean) => void;
  onMove: (by: number) => void;
}) {
  const { t } = useT();

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      {/* Neither the name nor its link can be cut short, so where the card is
          too narrow the key status and the actions wrap onto a line of their
          own. */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2.5">
        <NeutralSwitch on={enabled} onChange={onToggle} name={t('settings.captcha.enableSolver', { service: svc.label })} hue={hue} />

        <div className="flex flex-1 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-sm text-carbon-text">{svc.label}</span>
          {svc.whereUrl && <LinkBadge href={svc.whereUrl} title={t('accounts.whereToFind')} />}
        </div>

        <div className="ms-auto flex flex-wrap items-center justify-end gap-x-3 gap-y-2">
          <span
            className={`inline-flex shrink-0 items-center gap-1.5 text-meta font-medium ${state === 'set' ? 'text-statusOk' : 'text-carbon-textMuted'}`}
          >
            <span className={`h-1.5 w-1.5 rounded-[var(--radius-pill)] ${state === 'set' ? 'bg-statusOkSolid' : 'bg-carbon-textMuted/50'}`} />
            {t(KEY_STATE[state])}
            {state === 'off' && <InfoBubble tip={t('settings.captcha.offHint')} />}
          </span>

          {/* `labelled` on every badge, so the actions follow the Beschriftung
              setting like toolbar actions. */}
          <div className="flex flex-wrap items-center justify-end gap-0.5">
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
          </div>
        </div>
      </div>
    </li>
  );
}
