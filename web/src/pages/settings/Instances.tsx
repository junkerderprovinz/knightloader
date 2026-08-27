// The settings tab and the sidebar's own "Instanzen" destination point at the
// exact same page - this file only adds the one thing the sidebar entry point
// cannot offer a preference about itself: whether it exists at all (jdp,
// 2026-08-27: "Können wir den Instanzentab wie den konten-tab ein- und
// ausblendbar machen?"). Everything below the toggle card is pages/
// Instances.tsx, unmodified and unwrapped, not a second implementation that
// could drift from the first.
//
// Deliberately a copy of Accounts.tsx down to the missing gap on the wrapper,
// including that file's own reason for it: <Instances/> opens with its own
// `flex flex-col gap-10` and a PageHeader whose title is sr-only (zero visible
// height), which already reserves one full gap-10 before its first real card.
// A gap-10 here as well would stack a second 40px on top of the first.
import { Instances } from '../Instances';
import { Card, SectionTitle, ToggleRow } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { setHidden } from '../../lib/sidebarPrefs';
import { useDraft } from './context';

export function InstancesTab() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <div className="flex flex-col">
      <Card className="flex flex-col gap-3">
        <SectionTitle hue={0}>{t('settings.instances.setupTitle')}</SectionTitle>
        <ToggleRow
          label={t('settings.instances.showInSidebar')}
          hint={t('settings.instances.showInSidebarHint')}
          checked={!cfg.hideInstancesFromSidebar}
          onChange={(v) => {
            patch({ hideInstancesFromSidebar: !v });
            // Optimistic, ahead of the 600ms autosave - the sidebar reflects
            // the switch the moment it is flipped, not once the write lands.
            setHidden('instances', !v);
          }}
        />
      </Card>
      <Instances />
    </div>
  );
}
