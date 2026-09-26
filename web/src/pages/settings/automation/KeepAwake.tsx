import { ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useFeatures } from '../context';
import { ModuleToggle, ModulesPageBadge } from '../ModuleToggle';

/**
 * KeepAwakeRow is the switch that keeps the computer from sleeping while a
 * download runs, the same switch as the module's row on the Modules page. Only
 * the desktop build acts on it, so a container shows it off and disabled with
 * the reason in its (i), the way the automatic update install does on the
 * General page.
 */
export function KeepAwakeRow() {
  const { t } = useT();
  const module = useFeatures().features.modules.find((m) => m.id === 'keepawake');
  if (!module) return null;
  if (module.verdict === 'shipped') return <ModuleToggle id="keepawake" hint={t('settings.downloads.keepAwakeHint')} />;
  return (
    <ToggleRow
      label={t('settings.module.keepawake')}
      hint={t('settings.downloads.keepAwakeContainerHint')}
      checked={false}
      disabled
      onChange={() => {}}
      // The module's row has no switch here either, so the badge names the
      // page rather than claiming this switch is there as well.
      aside={<ModulesPageBadge m={module} title={t('settings.nav.modules')} />}
    />
  );
}
