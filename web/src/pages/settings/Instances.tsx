// The settings tile of the Instanzen page: the module's switch above the whole
// page, whether or not the page also stands in the sidebar; that switch is on
// the Aussehen page with the rest of the sidebar. The wrapper spaces its
// children itself because the page's sr-only header is positioned absolutely
// and takes no row.
import { Instances } from '../Instances';
import { Card, SectionTitle } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { ModuleToggle } from './ModuleToggle';

export function InstancesTab() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-3">
        <SectionTitle>{t('settings.module.federation')}</SectionTitle>
        <ModuleToggle id="federation" />
      </Card>
      <Instances firstHue={1} />
    </div>
  );
}
