// The settings tab shows the same page as the sidebar's Konten entry, with two
// cards on top: one hides that entry, one holds free downloads back. The
// wrapper spaces its children itself because the page's sr-only header is
// positioned absolutely and takes no row.
import { Accounts } from '../Accounts';
import { Card, SectionTitle, ToggleRow } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { setHidden } from '../../lib/sidebarPrefs';
import { useDraft } from './context';

export function AccountsTab() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <div className="flex flex-col gap-10">
      <Card hue={0} className="flex flex-col gap-3">
        <SectionTitle>{t('settings.accounts.setupTitle')}</SectionTitle>
        <ToggleRow
          label={t('settings.accounts.showInSidebar')}
          hint={t('settings.accounts.showInSidebarHint')}
          checked={!cfg.hideAccountsFromSidebar}
          onChange={(v) => {
            patch({ hideAccountsFromSidebar: !v });
            // The sidebar follows at once instead of after the autosave.
            setHidden('accounts', !v);
          }}
        />
      </Card>
      <Card hue={1} className="flex flex-col gap-3">
        <SectionTitle>{t('settings.accounts.freeTitle')}</SectionTitle>
        <ToggleRow
          label={t('settings.accounts.premiumOnly')}
          hint={t('settings.accounts.premiumOnlyHint')}
          checked={cfg.premiumOnly}
          onChange={(premiumOnly) => patch({ premiumOnly })}
        />
      </Card>
      <Accounts />
    </div>
  );
}
