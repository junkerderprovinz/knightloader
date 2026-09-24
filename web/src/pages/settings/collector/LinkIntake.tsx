import { useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { PathInput } from '../../../components/FolderPicker';
import { Card, InfoBubble, SectionTitle, ToggleRow } from '../../../components/ui';
import { IconChevronEnd } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import { WATCH_SUPPORTED } from '../../../lib/clipboardWatch';
import { useClipboardWatch } from '../../../lib/useClipboardWatch';
import { useDraft, useFeatures } from '../context';
import type { Feature } from '../features';
import { moduleDetail, moduleReason } from '../tx';

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
  const { cfg, patch, fieldError } = useDraft();
  const { features } = useFeatures();
  const [watch, setWatch] = useClipboardWatch();

  const cnl = features.modules.find((m) => m.id === 'cnl');
  const folderWatch = features.modules.find((m) => m.id === 'watch');

  // Switched off, the watch folder is parked on the server and cleared here,
  // so the field has nothing to show until the switch brings it back. With
  // nothing parked the field is how the module gets its first folder, and the
  // switch has nothing to turn on yet.
  const watchParked = !!folderWatch && !folderWatch.enabled && folderWatch.parked;
  const watchUnset = !!folderWatch && !folderWatch.enabled && !folderWatch.parked;

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.linkIntakeHint')}>{t('settings.sectionLinkIntake')}</SectionTitle>

      {cnl && (
        <ModuleSwitch
          m={cnl}
          hue={0}
          label={t('settings.module.cnl')}
          // Where the build has no switch, the server's reason says why. The
          // live reading, such as the address it listens on, matters where
          // JDownloader may already hold the port.
          hint={[
            cnl.switch === 'none' ? (moduleReason(t, cnl) ?? '') : t('settings.linkIntake.cnlHint'),
            moduleDetail(t, cnl) ?? '',
          ]}
        />
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
        hint={t('settings.autoStartHint')}
      />

      {folderWatch && (
        <ModuleSwitch
          m={folderWatch}
          hue={3}
          label={t('settings.module.watch')}
          hint={[
            t('settings.watchDirHint'),
            watchParked ? t('settings.linkIntake.watchParked') : '',
            watchUnset ? t('settings.linkIntake.watchPickFolder') : '',
          ]}
          blocked={watchUnset}
        >
          {!watchParked && (
            <PathInput
              value={cfg.watchDir}
              onValue={(watchDir) => patch({ watchDir })}
              error={fieldError('watchDir')}
              placeholder="/watch"
              title={t('settings.module.watch')}
              label={t('settings.module.watch')}
            />
          )}
        </ModuleSwitch>
      )}
    </Card>
  );
}

/**
 * ModuleSwitch is a module's switch on this card, the same switch as its row
 * on the Modules page, with the way there beneath it. `blocked` is for a state
 * the server would refuse, such as nothing parked to switch back on.
 */
function ModuleSwitch({
  m,
  hue,
  label,
  hint,
  blocked = false,
  children,
}: {
  m: Feature;
  hue: number;
  label: string;
  hint: string[];
  blocked?: boolean;
  children?: ReactNode;
}) {
  const { t } = useT();
  const { toggle } = useFeatures();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  // Keyed onto the row, so a switch the server refuses shakes again on every
  // refusal.
  const [shake, setShake] = useState(0);

  async function onSwitch(next: boolean) {
    setBusy(true);
    try {
      await toggle(m.id, next);
    } catch (e) {
      toast(t('settings.modules.switchFailed', { reason: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-1">
      <div key={shake} className={shake > 0 ? 'glim-shake' : undefined}>
        <ToggleRow
          hue={hue}
          label={label}
          hint={hint}
          checked={m.enabled}
          disabled={busy || blocked || m.verdict !== 'shipped' || m.switch === 'none'}
          onChange={(next) => void onSwitch(next)}
        />
      </div>
      <Link
        to="/settings/modules"
        className="flex w-fit items-center gap-1 text-[11px] text-carbon-textSub underline-offset-2 hover:text-carbon-text hover:underline focus-visible:underline"
      >
        {t('settings.modules.alsoOn', { page: t('settings.nav.modules') })}
        <IconChevronEnd className="h-3 w-3 rtl:-scale-x-100" aria-hidden />
      </Link>
      {children && <div className="mt-1">{children}</div>}
    </div>
  );
}
