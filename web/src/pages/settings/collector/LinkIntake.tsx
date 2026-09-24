import { useState } from 'react';
import { Card, Field, FieldGroup, InfoBubble, SectionTitle, TextInput, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import { WATCH_SUPPORTED } from '../../../lib/clipboardWatch';
import { useClipboardWatch } from '../../../lib/useClipboardWatch';
import { useDraft, useFeatures } from '../context';

/**
 * LinkIntakeCard holds the ways a link reaches the collector without being
 * typed. Click'n'Load is a server listener switched over the API, the same
 * state the Modules page shows. The clipboard watch runs in this tab only and
 * is not offered where the browser cannot read the clipboard, such as a
 * plain-HTTP LAN address; the row points at Ctrl+V instead.
 */
export function LinkIntakeCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features, toggle } = useFeatures();
  const { toast } = useToast();
  const [watch, setWatch] = useClipboardWatch();
  const [busy, setBusy] = useState(false);

  const cnl = features.modules.find((m) => m.id === 'cnl');
  const cnlSwitchable = !!cnl && cnl.verdict === 'shipped' && cnl.switch !== 'none';

  // The module registry decides whether the folder field is live, so it cannot
  // disagree with the Modules page. `parked` rather than `!enabled`, because an
  // empty folder on a fresh install also reads as off.
  const folderWatch = features.modules.find((m) => m.id === 'watch');
  const folderWatchOff = folderWatch !== undefined && !folderWatch.enabled && folderWatch.parked;

  // Keyed onto the row, so a switch the server refuses shakes again on every
  // refusal.
  const [cnlShake, setCnlShake] = useState(0);

  async function onCnl(next: boolean) {
    setBusy(true);
    try {
      await toggle('cnl', next);
    } catch (e) {
      toast(t('settings.modules.switchFailed', { reason: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setCnlShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.linkIntakeHint')}>{t('settings.sectionLinkIntake')}</SectionTitle>

      {cnl && (
        <div className="flex flex-col gap-1">
          <div key={cnlShake} className={cnlShake > 0 ? 'glim-shake' : undefined}>
            <ToggleRow
              hue={0}
              label={t('settings.module.cnl')}
              hint={cnl.reason || undefined}
              checked={cnl.enabled}
              disabled={!cnlSwitchable || busy}
              onChange={(next) => void onCnl(next)}
            />
          </div>
          {/* The live reading, such as the address it listens on, which matters
              where JDownloader may already hold the port. */}
          {cnl.detail && (
            <span className="text-[11px] text-carbon-textMuted" dir="ltr">
              {cnl.detail}
            </span>
          )}
        </div>
      )}

      {WATCH_SUPPORTED ? (
        <ToggleRow
          hue={1}
          label={t('intake.clipboardWatch')}
          hint={t('intake.clipboardWatchHint')}
          checked={watch}
          onChange={setWatch}
        />
      ) : (
        <div className="flex flex-col gap-1">
          <span className="flex items-center text-sm text-carbon-text">
            {t('intake.clipboardWatch')}
            <InfoBubble tip={t('intake.clipboardWatchHint')} />
          </span>
          <span className="text-[11px] text-carbon-textMuted">
            {t('intake.clipboardWatchUnavailable')}
          </span>
        </div>
      )}

      <ToggleRow
        hue={2}
        checked={cfg.autoConfirm}
        onChange={(v) => patch({ autoConfirm: v })}
        label={t('settings.autoStart')}
      />

      {/* While the module is parked the folder is cleared, so the field becomes
          a reading that names the switch on the Modules page. */}
      {folderWatchOff ? (
        <FieldGroup label={t('settings.watchDir')} hint={t('settings.watchDirHint')}>
          <span className="text-sm text-carbon-textSub">{t('settings.downloads.watchOff')}</span>
        </FieldGroup>
      ) : (
        <Field label={t('settings.watchDir')} hint={t('settings.watchDirHint')}>
          <TextInput
            dir="ltr"
            value={cfg.watchDir}
            placeholder="/watch"
            spellCheck={false}
            onChange={(e) => patch({ watchDir: e.target.value })}
          />
        </Field>
      )}
    </Card>
  );
}
