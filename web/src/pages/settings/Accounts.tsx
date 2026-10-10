// The settings tile of the Konten page. While the page stands in the sidebar
// the tile keeps the switch that put it there and the way to the page, and with
// the switch off it draws the whole page. The wrapper spaces its children itself
// because the page's sr-only header is positioned absolutely and takes no row.
import { Accounts } from '../Accounts';
import { Card, SectionTitle, ToggleRow } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { setHidden } from '../../lib/sidebarPrefs';
import { DriveCard } from './accounts/DriveCard';
import { useDraft } from './context';
import { OpenPageRow, useWholePage } from './controls';

export function AccountsTab() {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const pinned = !cfg.hideAccountsFromSidebar;
  const whole = useWholePage('accounts', pinned);

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-3">
        <SectionTitle>{t('settings.accounts.setupTitle')}</SectionTitle>
        <ToggleRow
          label={t('settings.accounts.showInSidebar')}
          hint={t('settings.accounts.showInSidebarHint')}
          checked={pinned}
          onChange={(v) => {
            patch({ hideAccountsFromSidebar: !v });
            // The sidebar follows at once instead of after the autosave.
            setHidden('accounts', !v);
          }}
        />
        {pinned && <OpenPageRow label={t('nav.accounts')} to="/accounts" />}
      </Card>
      {whole && <Accounts />}
      <DriveCard hue={4} />
    </div>
  );
}
