// The settings tab and the sidebar's own "Konten" destination point at the
// exact same page (jdp: "Beide Tabs sollen dann das gleiche anzeigen") -
// this file only adds the one thing the sidebar entry point cannot offer a
// preference about itself: whether it exists at all. Everything else below
// the toggle card is pages/Accounts.tsx, unmodified and unwrapped, not a
// second implementation that could drift from the first.
//
// gap-10 on the wrapper, the house gap every other settings page's own root
// carries. It stood here WITHOUT one, on the argument that <Accounts/>'s
// sr-only PageHeader was an invisible flex row already earning a gap-10 of its
// own, so a gap here would stack a second 40px on top of it.
//
// That argument was wrong about the one thing it rested on: sr-only positions
// the header ABSOLUTELY, and an absolutely positioned child is not a flex item
// at all - it takes no row and earns no gap. PageHeader says so itself, in the
// comment that explains why it is sr-only rather than `hidden` (components/
// ui.tsx). So the wrapper's two children sat at zero distance and the toggle
// card was glued to the debrid card below it (jdp: "in der instanzen und konten
// tab in den einstellungen ist die oberste card verklebt mit dem darunter").
// The invisible header changes nothing here either way; it is the wrapper that
// has to space its own two children, exactly like any other card stack.
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
            // Optimistic, ahead of the 600ms autosave - the sidebar reflects
            // the switch the moment it is flipped, not once the write lands.
            setHidden('accounts', !v);
          }}
        />
      </Card>
      <Accounts />
    </div>
  );
}
