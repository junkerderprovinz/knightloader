import { useRef, type ReactNode } from 'react';
import { type Account, ApiError, type CatalogueService, createTestCaptcha } from '../../../lib/api';
import { type TranslationKey, useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import { Card, Field, InfoBubble, LinkBadge, NumberInput, SectionTitle, ToggleRow, type ButtonVerdict } from '../../../components/ui';
import { HosterIcon } from '../../../components/HosterIcon';
import { CaptchaChain, type ChainSolver } from '../../../components/pictures/CaptchaChain';
import { LIFT, SETTLE, useReorder } from '../../../components/dragLift';
import { RowGrip, moveTo } from '../../../components/RowGrip';
import { TestButton } from '../../../components/TestButton';
import { useDraft, useFeatures } from '../context';
import { NeutralSwitch, SubHead } from '../controls';
import { ModuleToggle } from '../ModuleToggle';

// The captcha cards order the captcha accounts and say whether they wait for
// somebody watching. They stand on the Accounts page beside the accounts whose
// keys they read. The order lives in the settings: an id in captchaSolverOrder
// is tried in that position, an absent one never.

// Copies of the bounds in internal/settings/settings_captcha.go, which no route
// serves.
const MIN_WAIT = 10;
const MAX_WAIT = 600;

/**
 * CaptchaCards draws the order card on `hue` and the card on when the accounts
 * start on the one after it. `accounts` and `catalogue` are the Accounts
 * page's own, so a key saved there shows here at once.
 */
export function CaptchaCards({
  hue,
  accounts,
  catalogue,
}: {
  hue: number;
  accounts: Account[];
  catalogue: CatalogueService[];
}) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();
  const list = useRef<HTMLUListElement>(null);

  const order = cfg.captchaSolverOrder ?? [];
  const setOrder = (next: string[]) => patch({ captchaSolverOrder: next.length > 0 ? next : null });
  // Only the grip starts a drag, so a mouse arms it by moving; the switch and
  // the link in a row keep their own clicks.
  const drag = useReorder({
    ids: order,
    container: list,
    attr: 'data-solver-id',
    axis: 'y',
    arm: 'move',
    enabled: true,
    onReorder: setOrder,
  });

  const solvers = catalogue.filter((s) => s.group === 'captchaSolver');
  // Enabled solvers in the chosen order, then the others in catalogue order,
  // so switching one off moves it to the bottom.
  const enabled = drag.order.map((id) => solvers.find((s) => s.id === id)).filter((s): s is CatalogueService => Boolean(s));
  const rows = [...enabled, ...solvers.filter((s) => !order.includes(s.id))];

  const chain: ChainSolver[] = [
    ...order.map((id) => solvers.find((s) => s.id === id)).filter((s): s is CatalogueService => Boolean(s)),
    ...solvers.filter((s) => !order.includes(s.id)),
  ].map((svc) => {
    const state = keyState(svc.id, accounts);
    return {
      id: svc.id,
      name: svc.label,
      place: order.indexOf(svc.id) + 1,
      skipped: state === 'set' ? undefined : t(KEY_STATE[state]),
    };
  });
  const switchedOff = features.modules.some((m) => m.id === 'captcha' && !m.enabled);

  function setEnabled(id: string, on: boolean) {
    if (on) {
      if (!order.includes(id)) setOrder([...order, id]);
    } else {
      setOrder(order.filter((x) => x !== id));
    }
  }

  return (
    <>
      <Card hue={hue} data-new="captcha-cards" className="flex flex-col gap-1">
        <SectionTitle hint={t('settings.captcha.orderHint')}>{t('settings.captcha.orderTitle')}</SectionTitle>
        <ModuleToggle id="captcha" />
        {order.length === 0 && <p className="py-2 text-sm text-carbon-textSub">{t('settings.captcha.orderEmpty')}</p>}

        {/* The chain stands beside the rows it is drawn from, and under them
            where the card is too narrow for both. */}
        <div className="@container">
          <div className="flex flex-col gap-x-8 gap-y-4 @[46rem]:flex-row @[46rem]:items-start">
            {/* The list is the rows' offsetParent, the layout a drag measures
                in. While a row is held the others wiggle (glim-drag-armed). */}
            <ul ref={list} className={`relative flex min-w-0 flex-1 flex-col ${drag.held !== null ? 'glim-drag-armed' : ''}`}>
              {rows.map((svc, i) => {
                const on = order.includes(svc.id);
                const look = on ? drag.look(svc.id) : '';
                return (
                  <SolverRow
                    key={svc.id}
                    svc={svc}
                    hue={i}
                    enabled={on}
                    last={i === rows.length - 1}
                    state={keyState(svc.id, accounts)}
                    onToggle={(next) => setEnabled(svc.id, next)}
                    look={look}
                    grip={
                      on && (
                        <RowGrip
                          label={t('accounts.routing.dragHandle', { name: svc.label })}
                          disabled={false}
                          onMove={(by) => {
                            const next = moveTo(order, svc.id, by);
                            if (next) setOrder(next);
                          }}
                          onPointerDown={(e) => drag.press(e, svc.id)}
                        />
                      )
                    }
                  />
                );
              })}
            </ul>
            <div className="w-full shrink-0 @[46rem]:w-[18rem]">
              <CaptchaChain
                solvers={chain}
                wait={cfg.captchaSolverOnlyUnwatched ? cfg.captchaSolverWait : null}
                off={switchedOff}
                label={t('settings.captcha.orderTitle')}
              />
            </div>
          </div>
        </div>
        <TestCaptcha solvers={canTakeATest(order, accounts)} />
      </Card>

      {/* Absent while no solver is enabled, since nothing reads these then. */}
      {order.length > 0 && (
        <Card hue={hue + 1} className="flex flex-col gap-5">
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
    </>
  );
}

/**
 * canTakeATest reports whether one of the solvers in order would get a test
 * captcha: it needs a key and must not be switched off among the captcha
 * accounts, as the instance's captchaSolvers decides.
 */
export function canTakeATest(order: string[], accounts: Account[]): boolean {
  return order.some((id) => keyState(id, accounts) === 'set');
}

type KeyState = 'set' | 'off' | 'notSet';

/**
 * keyState says how the solver's account stands among the captcha accounts.
 * A key on a switched-off account counts as off, since the instance hands that
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
function TestCaptcha({ solvers }: { solvers: boolean }) {
  const { t } = useT();
  const { toast } = useToast();
  const words = { ok: t('test.sent'), fail: t('test.failed') };

  // The button says whether the captcha went up; why it did not is a toast.
  const send = (toSolvers: boolean) => async (): Promise<ButtonVerdict> => {
    try {
      await createTestCaptcha(toSolvers);
      return 'ok';
    } catch (e) {
      toast(t(testRefusal(e)), 'fail');
      return 'fail';
    }
  };

  return (
    <div className="flex flex-col gap-3 pt-4">
      <SubHead label={t('settings.captcha.testTitle')} />
      <div className="flex flex-wrap gap-2">
        <TestButton label={t('settings.captcha.test')} hint={t('settings.captcha.testHint')} words={words} run={send(false)} />
        {solvers && (
          <TestButton
            label={t('settings.captcha.testSolvers')}
            hint={t('settings.captcha.testSolversHint')}
            words={words}
            run={send(true)}
          />
        )}
      </div>
    </div>
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
  last,
  state,
  onToggle,
  look = '',
  grip,
}: {
  svc: CatalogueService;
  /** Position in the full solver list, for the hue. */
  hue: number;
  enabled: boolean;
  last: boolean;
  state: KeyState;
  onToggle: (on: boolean) => void;
  /** The drag class of an enabled row; see useReorder. */
  look?: string;
  /** The grip that moves an enabled row, absent on one that is switched off. */
  grip?: ReactNode;
}) {
  const { t } = useT();
  const carried = look === LIFT || look === SETTLE;

  return (
    <li
      data-solver-id={enabled ? svc.id : undefined}
      // A carried row floats over the others, so it takes a ground of its own.
      className={`select-none rounded-[var(--radius-control)] ${last ? '' : 'border-b border-carbon-border/60'} ${
        carried ? 'bg-carbon-surface2' : ''
      } ${look}`}
    >
      {/* Neither the name nor its link can be cut short, so where the card is
          too narrow the key status wraps onto a line of its own. */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2.5">
        {/* A switched-off row keeps the grip's room, so the switches stand in one column. */}
        <span className="flex w-6 shrink-0 justify-center">{grip}</span>
        <NeutralSwitch on={enabled} onChange={onToggle} name={t('settings.captcha.enableSolver', { service: svc.label })} hue={hue} />

        <div className="flex flex-1 flex-wrap items-center gap-x-2 gap-y-1">
          {/* The service's mark, as in the accounts table and the picker. */}
          <HosterIcon host={svc.whereUrl} />
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
        </div>
      </div>
    </li>
  );
}
