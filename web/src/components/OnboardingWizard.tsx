import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { fetchSettings, patchSettings, type Settings } from '../lib/api';
import { useT } from '../lib/i18n';
import { IconClose } from '../lib/icons';
import { readUIState, useUIState } from '../lib/uistate';
import { Button, Field, LoadingCard, Modal } from './ui';
import { PathInput } from './FolderPicker';
import { refusalText } from '../pages/settings/tx';
import { LanguagePicker } from './LanguagePicker';

const STEPS = ['welcome', 'folder', 'accounts', 'finished'] as const;
type Step = (typeof STEPS)[number];

/**
 * OnboardingWizard is the first-run tour, mounted once in app/Layout.tsx. The
 * done flag lives in the server-persisted uistate, so dismissing it on one
 * browser covers all of them.
 */
export function OnboardingWizard() {
  const { t } = useT();
  const navigate = useNavigate();
  const [done, setDone] = useUIState<boolean>('onboarding.done', false);
  // useUIState answers its fallback until the document loads, which would
  // flash the tour at returning users.
  const [ready, setReady] = useState(false);
  const [stepIndex, setStepIndex] = useState(0);

  // Fetched up front so the folder step does not open on a loading state.
  const [settings, setSettings] = useState<Settings | null>(null);
  const [downloadDir, setDownloadDir] = useState('');
  const [folderError, setFolderError] = useState<string | undefined>();

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
        // The folder step falls back to an empty field.
      },
    );
    return () => {
      live = false;
    };
  }, [ready, done, settings]);

  if (!ready || done) return null;

  function close() {
    // A folder set before skipping is kept. Best effort: Settings still has it.
    if (settings && downloadDir.trim() !== '' && downloadDir.trim() !== settings.downloadDir) {
      void patchSettings({ downloadDir: downloadDir.trim() }).catch(() => {});
    }
    setDone(true);
  }

  /**
   * saveFolder stores the folder on the way past its step, so a refusal shows
   * beside the field instead of being lost when the tour closes.
   */
  async function saveFolder(): Promise<boolean> {
    const dir = downloadDir.trim();
    if (!settings || dir === '' || dir === settings.downloadDir) return true;
    try {
      setSettings(await patchSettings({ downloadDir: dir }));
      return true;
    } catch (e) {
      setFolderError(refusalText(t, e));
      return false;
    }
  }

  async function next() {
    if (step === 'folder' && !(await saveFolder())) return;
    setStepIndex((i) => i + 1);
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
  // The welcome and the closing words are what their steps are for, so they
  // stay on the page; the other two explain the control below them.
  const hintKey = {
    welcome: undefined,
    folder: 'onboarding.folder.body',
    accounts: 'onboarding.accounts.body',
    finished: undefined,
  } as const;

  return (
    <Modal
      title={t(titleKey[step])}
      hint={hintKey[step] && t(hintKey[step])}
      onClose={close}
      footer={
        <>
          {/* Skip sits at the start, away from Next. It is the way out, so it
              carries the close glyph. */}
          <Button kind="ghost" labelled icon={<IconClose />} title={t('onboarding.skip')} onClick={close} />
          <span className="flex-1" />
          {stepIndex > 0 && (
            <Button kind="secondary" onClick={() => setStepIndex((i) => i - 1)}>
              {t('onboarding.back')}
            </Button>
          )}
          <Button kind="primary" onClick={last ? close : () => void next()}>
            {last ? t('onboarding.finish') : t('onboarding.next')}
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

        {step === 'folder' &&
          (settings ? (
            <Field label={t('settings.downloadDir')} hint={t('settings.downloadDirHint')}>
              <PathInput
                value={downloadDir}
                placeholder="/downloads"
                onValue={(dir) => {
                  setDownloadDir(dir);
                  setFolderError(undefined);
                }}
                error={folderError}
              />
            </Field>
          ) : (
            <LoadingCard nested label={t('common.loading')} />
          ))}

        {step === 'accounts' && (
          <Button kind="secondary" className="w-fit" onClick={openAccounts}>
            {t('onboarding.accounts.link')}
          </Button>
        )}

        {step === 'finished' && <p className="text-sm text-carbon-textSub">{t('onboarding.finished.body')}</p>}
      </div>
    </Modal>
  );
}
