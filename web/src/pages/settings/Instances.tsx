// The settings tab and the sidebar's own "Instanzen" destination point at the
// exact same page - this file only adds the one thing the sidebar entry point
// cannot offer a preference about itself: whether it exists at all (jdp,
// 2026-08-27: "Können wir den Instanzentab wie den konten-tab ein- und
// ausblendbar machen?"). Everything below the toggle card is pages/
// Instances.tsx, unmodified and unwrapped, not a second implementation that
// could drift from the first.
//
// Deliberately a copy of Accounts.tsx, gap-10 on the wrapper included - see
// that file for why the gap has to be here: the sr-only PageHeader <Instances/>
// opens with is positioned absolutely, so it is not a flex item, takes no row
// and earns no gap. Both tabs were written on the belief that it did, and both
// showed the same fault: the toggle card glued to the first card of the page
// below it (jdp: "in der instanzen und konten tab in den einstellungen ist die
// oberste card verklebt mit dem darunter").
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
