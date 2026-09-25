import { ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useDraft } from '../context';

/**
 * KeepAwakeRow is the switch that keeps the computer from sleeping while a
 * download runs. Only the desktop build acts on it, so a container shows it
 * off and disabled with the reason in its (i), the way the automatic update
 * install does on the General page.
 */
export function KeepAwakeRow({ deployment }: { deployment: string }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  // Wait for the deployment rather than flash the container's copy.
  if (deployment === '') return null;
  const desktop = deployment === 'desktop';
  return (
    <ToggleRow
      label={t('settings.downloads.keepAwake')}
      hint={desktop ? t('settings.downloads.keepAwakeHint') : t('settings.downloads.keepAwakeContainerHint')}
      checked={desktop && cfg.keepAwake}
      disabled={!desktop}
      onChange={(v) => patch({ keepAwake: v })}
    />
  );
}
