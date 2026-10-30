import type { ReactNode } from 'react';
import { type Account, ApiError, type CatalogueService, createTestCaptcha } from '../../../lib/api';
import { type TranslationKey, useT } from '../../../lib/i18n';
import { IconCaptcha, IconSendTest } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';
import { EmptyState, NumberInput, ToggleRow, type ButtonVerdict } from '../../../components/ui';
import { PriorityCard, PriorityLadder } from '../../../components/PriorityLadder';
import { TestButton } from '../../../components/TestButton';
import { useDraft, useFeatures } from '../context';
import { SettingRow, SubHead } from '../controls';
import { ModuleToggle } from '../ModuleToggle';

// The captcha priority card orders the connected captcha accounts the way the
// download priority card beside it orders the backends, and holds when they
// start and the test captcha, all under the captcha module's switch. The order
// lives in the settings and only says who goes first: every captcha account
// with a key that is switched on is tried, the ones in captchaSolverOrder
// first.

// Copies of the bounds in internal/settings/settings_captcha.go, which no route
// serves.
const MIN_WAIT = 10;
const MAX_WAIT = 600;

/**
 * connectedSolvers are the captcha accounts that get a captcha, in the order
 * the instance tries them: those in `order` first, the rest in catalogue
 * order. One without a key, or switched off among the captcha accounts, gets
 * none and is left out.
 */
export function connectedSolvers(order: string[], accounts: Account[], catalogue: CatalogueService[]): CatalogueService[] {
  const solvers = catalogue.filter((s) => s.group === 'captchaSolver' && solverAccount(s.id, accounts));
  const place = (id: string) => {
    const at = order.indexOf(id);
    return at < 0 ? order.length : at;
  };
  return solvers
    .map((s, i) => ({ s, i }))
    .sort((a, b) => place(a.s.id) - place(b.s.id) || a.i - b.i)
    .map(({ s }) => s);
}

/**
 * solverAccount is the account whose key the instance hands a solver's
 * captchas to: the unnamed one, or else the first named one that is switched
 * on, the order app.routedAccounts reads them in.
 */
export function solverAccount(id: string, accounts: Account[]): Account | undefined {
  return accounts
    .filter((a) => a.service === id && a.configured && a.enabled)
    .sort((x, y) => (x.account < y.account ? -1 : x.account > y.account ? 1 : 0))[0];
}

/** CaptchaSwitch is the captcha module's switch above the card's rows, where there is a switch to show. */
function CaptchaSwitch({ on, hint, children }: { on: boolean; hint: string; children: ReactNode }) {
  return on ? (
    <ModuleToggle id="captcha" hint={hint}>
      {children}
    </ModuleToggle>
  ) : (
    <>{children}</>
  );
}

/**
 * CaptchaPriorityCard draws the captcha priority card on `hue`. `accounts`
 * and `catalogue` are the Accounts page's own, so a key saved there shows here
 * at once.
 */
export function CaptchaPriorityCard({
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
  // Without a JDownloader backend no captcha can arrive and the module has no
  // switch, so the card says why in its bubble rather than under an inert one.
  const noSource = features.modules.find((m) => m.id === 'captcha')?.switch === 'none';

  const order = cfg.captchaSolverOrder ?? [];
  const solvers = connectedSolvers(order, accounts, catalogue);
  const labelOf = new Map(solvers.map((s) => [s.id, s.label]));
  // A key that failed its check stays in the order, since it is still tried.
  const troubleFor = (id: string) => {
    const a = solverAccount(id, accounts);
    return a && a.detail && !a.ok ? t('accounts.routing.checkFailed', { reason: a.detail }) : undefined;
  };
  // An account whose key is gone keeps its place, for when it comes back.
  const reorder = (next: string[]) =>
    patch({ captchaSolverOrder: [...next, ...order.filter((id) => !next.includes(id))] });
  const title = t('settings.captcha.orderTitle');

  return (
    <PriorityCard
      hue={hue}
      data-new="captcha-priority"
      title={title}
      hint={[
        t('settings.captcha.orderHint', { accounts: t('accounts.captcha.title') }),
        noSource ? t('settings.modules.reason.captchaNoJD') : '',
      ].join(' ').trim()}
      intro={t('settings.captcha.orderIntro')}
    >
      <CaptchaSwitch on={!noSource} hint={t('settings.modules.about.captcha')}>
        <div data-new="priority-accounts">
          {solvers.length === 0 ? (
            <EmptyState
              nested
              icon={<IconCaptcha width={26} height={26} />}
              title={t('settings.captcha.orderEmpty')}
            />
          ) : (
            <PriorityLadder
              ids={solvers.map((s) => s.id)}
              title={title}
              entry={t('settings.captcha.orderEntry')}
              labelFor={(id) => labelOf.get(id) ?? id}
              troubleFor={troubleFor}
              glyph={IconCaptcha}
              onReorder={reorder}
            />
          )}
        </div>

        {/* Nothing reads these while no captcha account is connected. */}
        {solvers.length > 0 && (
          <div className="flex flex-col gap-4 pt-4">
            <SubHead label={t('settings.captcha.whenTitle')} />
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
              <SettingRow control="short" label={t('settings.captcha.wait')} hint={t('settings.captcha.waitHint')}>
                <NumberInput
                  value={cfg.captchaSolverWait}
                  min={MIN_WAIT}
                  max={MAX_WAIT}
                  step={10}
                  onValue={(v) => patch({ captchaSolverWait: v })}
                />
              </SettingRow>
            )}
          </div>
        )}
        <TestCaptcha solvers={solvers.length > 0} />
      </CaptchaSwitch>
    </PriorityCard>
  );
}

const TEST_REFUSALS: Partial<Record<string, TranslationKey>> = {
  captchaOff: 'settings.captcha.testOff',
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
        <TestButton
          icon={<IconSendTest />}
          label={t('settings.captcha.test')}
          hint={t('settings.captcha.testHint')}
          words={words}
          run={send(false)}
        />
        {solvers && (
          <TestButton
            icon={<IconSendTest />}
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
