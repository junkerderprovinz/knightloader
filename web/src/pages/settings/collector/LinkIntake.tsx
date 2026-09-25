import { Card, InfoBubble, SectionTitle, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { WATCH_SUPPORTED } from '../../../lib/clipboardWatch';
import { useClipboardWatch } from '../../../lib/useClipboardWatch';
import { useDraft, useFeatures } from '../context';
import { ModuleToggle } from '../ModuleToggle';
import { SettingPathInput } from '../controls';

/**
 * LinkIntakeCard holds the ways a link reaches the collector without being
 * typed. Click'n'Load and the watched folder are modules, switched through the
 * registry like their rows on the Modules page, so each is one switch in two
 * places. The clipboard watch runs in this tab only and is not offered where
 * the browser cannot read the clipboard, such as a plain-HTTP LAN address; the
 * row points at Ctrl+V instead.
 */
export function LinkIntakeCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();
  const [watch, setWatch] = useClipboardWatch();

  const cnl = features.modules.find((m) => m.id === 'cnl');
  const folderWatch = features.modules.find((m) => m.id === 'watch');

  // Switched off, the watch folder is parked on the server and cleared here,
  // so the field has nothing to show until the switch brings it back. With
  // nothing parked the field is how the module gets its first folder.
  const watchParked = !!folderWatch && !folderWatch.enabled && folderWatch.parked;

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.linkIntakeHint')}>{t('settings.sectionLinkIntake')}</SectionTitle>

      {/* Where the build has no switch, the server's reason stands alone. The
          live reading the switch adds, such as the address it listens on,
          matters where JDownloader may already hold the port. */}
      <ModuleToggle
        id="cnl"
        hue={0}
        hint={cnl?.switch === 'none' ? undefined : t('settings.linkIntake.cnlHint')}
      />

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
        hint={t('settings.autoStartHint')}
      />

      <ModuleToggle
        id="watch"
        hue={3}
        hint={t('settings.watchDirHint')}
        setUpHint={t('settings.linkIntake.watchPickFolder')}
        parkedHint={t('settings.linkIntake.watchParked')}
      >
        {!watchParked && (
          <SettingPathInput
            field="watchDir"
            value={cfg.watchDir}
            onValue={(watchDir) => patch({ watchDir })}
            placeholder="/watch"
            title={t('settings.module.watch')}
            label={t('settings.module.watch')}
          />
        )}
      </ModuleToggle>
    </Card>
  );
}
