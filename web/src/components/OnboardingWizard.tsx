import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { fetchSettings, patchSettings, type Settings } from '../lib/api';
import { useT } from '../lib/i18n';
import { readUIState, useUIState } from '../lib/uistate';
import { Button, Field, LoadingCard, Modal } from './ui';
import { PathInput } from './FolderPicker';
import { LanguagePicker } from './LanguagePicker';

// OnboardingWizard is the first-run tour: welcome, download folder, a pointer
// at Accounts, done. Mounted once in app/Layout.tsx, beside CaptchaModal and
// IdleActionBanner, for the same reason those two live there rather than
// inside a page - see Layout.tsx's own comments on each. This one has
// nothing to do with which route is open either: it is gated on a single
// flag, not on navigation, so mounting it inside the keyed page div would
// remount (and restart) it on the first click anywhere.
//
// 'onboarding.done' lives in the shared uistate bucket - lib/uistate.ts's
// own doc comment is the reason: server-persisted, so a person who dismissed
// the tour on one browser does not see it again on another, and it survives
// a reload without a new storage mechanism being invented for it.
//
// The gate waits for readUIState() to resolve before deciding anything, the
// same rule pages/Settings.tsx's RememberedPage already follows for its own
// remembered field: useUIState hands out its `false` fallback until the
// document loads, and rendering the tour on that fallback would flash it in
// front of every returning user for one frame before the real value (already
// true, weeks ago) arrived and took it away again.
const STEPS = ['welcome', 'folder', 'accounts', 'finished'] as const;
type Step = (typeof STEPS)[number];

export function OnboardingWizard() {
  const { t } = useT();
  const navigate = useNavigate();
  const [done, setDone] = useUIState<boolean>('onboarding.done', false);
  const [ready, setReady] = useState(false);
  const [stepIndex, setStepIndex] = useState(0);

  // The live settings, fetched once - only the folder step reads from it, but
  // fetching lazily on arrival at that step would mean the field sits on
  // "loading" for a moment on every visit, and this component only ever
  // shows for one session per install: one GET up front is cheaper than the
  // bookkeeping needed to fetch it later.
  const [settings, setSettings] = useState<Settings | null>(null);
  const [downloadDir, setDownloadDir] = useState('');

  useEffect(() => {
    let live = true;
    readUIState().then(() => live && setReady(true));
    return () => {
      live = false;
    };
  }, []);

  useEffect(() => {
    if (!ready || done || settings) return;
    let live = true;
    fetchSettings().then(
      (s) => {
        if (!live) return;
        setSettings(s);
        setDownloadDir(s.downloadDir);
      },
      () => {
        /* the folder step falls back to an empty, still-usable field */
      },
    );
    return () => {
      live = false;
    };
  }, [ready, done, settings]);

  if (!ready || done) return null;

  function close() {
    // Whatever folder was typed or picked is worth keeping even if the tour
    // is left early - a person who set the folder on step 2 and then hit
    // Skip should not have to set it again on the real Settings page.
    if (settings && downloadDir.trim() !== '' && downloadDir.trim() !== settings.downloadDir) {
      void patchSettings({ downloadDir: downloadDir.trim() }).catch(() => {
        // Best effort: the field is still there, editable, on Settings >
        // General - this is a courtesy save, not the only way to set it.
      });
    }
    setDone(true);
  }

  function openAccounts() {
    close();
    navigate('/accounts');
  }

  const step: Step = STEPS[stepIndex];
  const last = stepIndex === STEPS.length - 1;
  const titleKey = {
    welcome: 'onboarding.welcome.title',
    folder: 'onboarding.folder.title',
    accounts: 'onboarding.accounts.title',
    finished: 'onboarding.finished.title',
  } as const;

  return (
    <Modal
      title={t(titleKey[step])}
      onClose={close}
      footer={
        <>
          {stepIndex > 0 && (
            <Button kind="secondary" onClick={() => setStepIndex((i) => i - 1)}>
              {t('onboarding.back')}
            </Button>
          )}
          <Button kind="primary" onClick={last ? close : () => setStepIndex((i) => i + 1)}>
            {last ? t('onboarding.finish') : t('onboarding.next')}
          </Button>
          <span className="flex-1" />
          <Button kind="ghost" onClick={close}>
            {t('onboarding.skip')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <p className="sr-only" aria-live="polite">
          {t('onboarding.step', { n: stepIndex + 1, total: STEPS.length })}
        </p>
        <div className="flex items-center justify-center gap-1.5" aria-hidden="true">
          {STEPS.map((s, i) => (
            <span
              key={s}
              className={`h-1.5 w-1.5 rounded-[var(--radius-pill)] transition-colors ${
                i === stepIndex ? 'bg-accent' : 'bg-carbon-surface2'
              }`}
            />
          ))}
        </div>

        {step === 'welcome' && (
          <div className="flex flex-col gap-4">
            <p className="text-sm text-carbon-textSub">{t('onboarding.welcome.body')}</p>
            <Field label={t('onboarding.welcome.langLabel')}>
              <LanguagePicker
                standalone
                className="glim-well flex w-full items-center gap-2 px-3 py-2 text-sm text-carbon-text"
              />
            </Field>
          </div>
        )}

        {step === 'folder' && (
          <div className="flex flex-col gap-4">
            <p className="text-sm text-carbon-textSub">{t('onboarding.folder.body')}</p>
            {settings ? (
              <Field label={t('settings.downloadDir')} hint={t('settings.downloadDirHint')}>
                <PathInput value={downloadDir} placeholder="/downloads" onValue={setDownloadDir} />
              </Field>
            ) : (
              <LoadingCard nested label={t('common.loading')} />
            )}
          </div>
        )}

        {step === 'accounts' && (
          <div className="flex flex-col gap-4">
            <p className="text-sm text-carbon-textSub">{t('onboarding.accounts.body')}</p>
            <Button kind="secondary" className="w-fit" onClick={openAccounts}>
              {t('onboarding.accounts.link')}
            </Button>
          </div>
        )}

        {step === 'finished' && <p className="text-sm text-carbon-textSub">{t('onboarding.finished.body')}</p>}
      </div>
    </Modal>
  );
}
