// The settings tab shows the same page as the sidebar's Instanzen entry, with
// one card on top that hides that entry. The wrapper spaces its children itself
// because the page's sr-only header is positioned absolutely and takes no row.
import { Instances } from '../Instances';
import { Card, SectionTitle, ToggleRow } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { setHidden } from '../../lib/sidebarPrefs';
import { useDraft } from './context';

export function InstancesTab() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-3">
        <SectionTitle>{t('settings.instances.setupTitle')}</SectionTitle>
        <ToggleRow
          label={t('settings.instances.showInSidebar')}
          hint={t('settings.instances.showInSidebarHint')}
          checked={!cfg.hideInstancesFromSidebar}
          onChange={(v) => {
            patch({ hideInstancesFromSidebar: !v });
            // The sidebar follows at once instead of after the autosave.
            setHidden('instances', !v);
          }}
        />
      </Card>
      <Instances />
    </div>
  );
}
