import { useState } from 'react';
import { Button, Card, InfoBubble, SectionTitle, ToggleRow } from '../../../components/ui';
import { IconSignIn } from '../../../lib/icons';
import { useToast } from '../../../lib/toast';
import { Dropdown } from '../../../components/Dropdown';
import { WaysIn } from '../../../components/pictures/WaysIn';
import { fetchInstances } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { WATCH_SUPPORTED } from '../../../lib/clipboardWatch';
import { isDesktop } from '../../../lib/desktop';
import { useResource } from '../../../lib/useResource';
import { useClipboardWatchTarget } from '../../../lib/useClipboardWatch';
import { useWatchSwitch } from '../../../components/WatchElsewhere';
import { useDraft, useFeatures } from '../context';
import { takeOverCnL } from '../features';
import { ModuleToggle } from '../ModuleToggle';
import { switchRefusal } from '../tx';
import { SettingPathInput, SettingRow } from '../controls';
import { ClipboardPopupRows } from './ClipboardPopup';
import { useWays } from './ways';

/**
 * LinkIntakeCard holds the ways a link reaches the collector without being
 * typed. Click'n'Load and the watched folder are modules, switched here
 * through the registry. The clipboard watch runs in this tab, or in the desktop app also
 * while its window is hidden, and is not offered where the browser cannot read
 * the clipboard, such as a plain-HTTP LAN address; the row points at Ctrl+V
 * instead. Where there are other instances, copied links can go to one of
 * them. The picture above the switches shows every way in, the ones set up
 * elsewhere included, and leads to each.
 */
export function LinkIntakeCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();
  const { watch, flip, dialog } = useWatchSwitch();
  const [target, setTarget] = useClipboardWatchTarget();
  const { data: peers } = useResource(fetchInstances);
  const { ways, open } = useWays(watch, peers);
  const watchHint = isDesktop() ? t('intake.clipboardWatchHintDesktop') : t('intake.clipboardWatchHint');
  // A target that has gone offline stays listed, so it can still be seen and
  // changed.
  const targets = [
    { value: '', label: t('instances.thisInstance') },
    ...(peers ?? []).map((p) => ({ value: p.name, label: p.displayName || p.name })),
  ];
  if (target && !targets.some((o) => o.value === target)) targets.push({ value: target, label: target });

  const cnl = features.modules.find((m) => m.id === 'cnl');
  const { toast } = useToast();
  const [takingOver, setTakingOver] = useState(false);
  // Another KnightLoader on this computer holds the port; the server asks it
  // through the group to let go, and the row follows the broadcast.
  async function takeOver() {
    setTakingOver(true);
    try {
      await takeOverCnL();
    } catch (e) {
      toast(switchRefusal(t, e), 'fail');
    } finally {
      setTakingOver(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('settings.linkIntakeHint')}>{t('settings.sectionLinkIntake')}</SectionTitle>
      <WaysIn ways={ways} label={t('settings.sectionLinkIntake')} onPick={open} />

      {/* Where the build has no switch, the server's reason stands alone. The
          live reading the switch adds, such as the address it listens on,
          matters where JDownloader may already hold the port. */}
      <ModuleToggle
        id="cnl"
        hue={0}
        hint={cnl?.switch === 'none' ? undefined : t('settings.linkIntake.cnlHint')}
      />
      {cnl?.detailCode === 'cnlHeldBy' && (
        <div className="flex justify-end">
          <Button kind="secondary" icon={<IconSignIn />} disabled={takingOver} onClick={() => void takeOver()}>
            {t('settings.linkIntake.cnlTakeOver')}
          </Button>
        </div>
      )}

      {WATCH_SUPPORTED ? (
        <>
          <ToggleRow
            hue={1}
            label={t('intake.clipboardWatch')}
            hint={watchHint}
            checked={watch}
            onChange={flip}
          />
          {isDesktop() && watch && <ClipboardPopupRows />}
          {targets.length > 1 && (
            <SettingRow
              control="long"
              label={t('intake.clipboardWatchTarget')}
              hint={t('intake.clipboardWatchTargetHint')}
            >
              <Dropdown
                label={t('intake.clipboardWatchTarget')}
                value={target}
                onChange={setTarget}
                options={targets}
              />
            </SettingRow>
          )}
        </>
      ) : (
        <div className="flex flex-col gap-1">
          <span className="flex items-center text-sm text-carbon-text">
            {t('intake.clipboardWatch')}
            <InfoBubble tip={t('intake.clipboardWatchHint')} />
          </span>
          <span className="text-meta text-carbon-textMuted">
            {t('intake.clipboardWatchUnavailable')}
          </span>
        </div>
      )}
      {dialog}

      <ToggleRow
        hue={2}
        checked={cfg.autoConfirm}
        onChange={(v) => patch({ autoConfirm: v })}
        label={t('settings.autoStart')}
        hint={t('settings.autoStartHint')}
      />

      <ModuleToggle id="watch" hue={3} hint={t('settings.watchDirFilesHint')}>
        <SettingRow control="long" label={t('task.folder')}>
          <SettingPathInput
            field="watchDir"
            value={cfg.watchDir}
            onValue={(watchDir) => patch({ watchDir })}
            placeholder="/watch"
            title={t('settings.module.watch')}
            label={t('settings.module.watch')}
            missingHint={t('folders.newWatch')}
          />
        </SettingRow>
      </ModuleToggle>
    </Card>
  );
}
