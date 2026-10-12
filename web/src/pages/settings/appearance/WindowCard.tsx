import { useEffect, useState } from 'react';
import { Card, Field, SectionTitle, ToggleRow } from '../../../components/ui';
import { Dropdown } from '../../../components/Dropdown';
import { onWindowPrefs, setWindowPrefs, windowPrefs, type WindowPrefs } from '../../../lib/desktop';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';

// The window's and the tray icon's behaviour belongs to this installation on
// this computer, so the desktop app keeps it in its own file (desktop.json)
// and not in the settings every connected browser shares. The icon's menu
// offers the same choices under the same names.

/**
 * WindowCard sets what the desktop app's window and tray icon do. The caller
 * draws it in the desktop app only, since a browser tab has neither.
 */
export function WindowCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const [prefs, setPrefs] = useState<WindowPrefs | null>(null);

  useEffect(() => {
    let live = true;
    windowPrefs().then(
      (p) => live && setPrefs(p),
      () => {},
    );
    const stop = onWindowPrefs((p) => live && setPrefs(p));
    return () => {
      live = false;
      stop();
    };
  }, []);

  // No switches rather than a guess at their state while the app answers.
  if (!prefs) return null;

  const before = prefs;
  async function change(next: Partial<WindowPrefs>) {
    setPrefs({ ...before, ...next });
    try {
      setPrefs(await setWindowPrefs({ ...before, ...next }));
    } catch (e) {
      setPrefs(before);
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
    }
  }

  // Without a tray there is nowhere to hide the window, and the app ignores
  // these three until it has one.
  const noTray = !prefs.trayAvailable;

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.window.hint')}>{t('settings.window.title')}</SectionTitle>
      {noTray && <p className="text-sm text-carbon-textSub">{t('settings.window.noTray')}</p>}
      <ToggleRow
        hue={0}
        label={t('tray.startHidden')}
        hint={t('settings.window.startHiddenHint')}
        checked={prefs.startHidden}
        disabled={noTray}
        onChange={(v) => void change({ startHidden: v })}
      />
      <ToggleRow
        hue={1}
        label={t('tray.closeToTray')}
        hint={t('settings.window.closeToTrayHint')}
        checked={prefs.onClose === 'tray'}
        disabled={noTray}
        onChange={(v) => void change({ onClose: v ? 'tray' : 'exit' })}
      />
      <ToggleRow
        hue={2}
        label={t('tray.minimiseToTray')}
        hint={t('settings.window.minimiseToTrayHint')}
        checked={prefs.onMinimize === 'tray'}
        disabled={noTray}
        onChange={(v) => void change({ onMinimize: v ? 'tray' : 'taskbar' })}
      />
      <Field label={t('tray.captcha')} hint={t('settings.window.captchaHint')}>
        <Dropdown
          label={t('tray.captcha')}
          value={prefs.raiseOnAttention}
          options={[
            { value: 'off', label: t('tray.raiseOff') },
            { value: 'front', label: t('tray.raiseFront') },
            { value: 'front-focused', label: t('tray.raiseFocus') },
          ]}
          onChange={(v) => void change({ raiseOnAttention: v })}
        />
      </Field>
    </Card>
  );
}
