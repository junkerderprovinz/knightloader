// The settings tab and the sidebar's own "Konten" destination point at the
// exact same page (jdp: "Beide Tabs sollen dann das gleiche anzeigen") -
// this file only adds the one thing the sidebar entry point cannot offer a
// preference about itself: whether it exists at all. Everything else below
// the toggle card is pages/Accounts.tsx, unmodified and unwrapped, not a
// second implementation that could drift from the first.
import { Accounts } from '../Accounts';
import { Card, SectionTitle, ToggleRow } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { setHideAccounts } from '../../lib/sidebarPrefs';
import { useDraft } from './context';

export function AccountsTab() {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <div className="flex flex-col gap-10">
      <Card className="flex flex-col gap-3">
        <SectionTitle hue={0}>{t('settings.accounts.setupTitle')}</SectionTitle>
        <ToggleRow
          label={t('settings.accounts.showInSidebar')}
          hint={t('settings.accounts.showInSidebarHint')}
          checked={!cfg.hideAccountsFromSidebar}
          onChange={(v) => {
            patch({ hideAccountsFromSidebar: !v });
            // Optimistic, ahead of the 600ms autosave - the sidebar reflects
            // the switch the moment it is flipped, not once the write lands.
            setHideAccounts(!v);
          }}
        />
      </Card>
      <Accounts />
    </div>
  );
}
