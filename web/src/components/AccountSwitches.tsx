// The gear at the end of an account card's heading. Accounts are enabled and
// disabled here, one by one in a short list or all at once, so a row only
// shows what an account is and says when it is disabled.
import { useRef, useState } from 'react';
import { useT } from '../lib/i18n';
import { IconGear } from '../lib/icons';
import { AnchoredPanel } from './AnchoredPanel';
import { ContextMenu, anchorBelow, useContextMenu } from './ContextMenu';
import { HosterIcon } from './HosterIcon';
import { IconBadge, Toggle } from './ui';

/** One account a card can enable and disable. */
export interface SwitchableAccount {
  key: string;
  label: string;
  /** Whatever HosterIcon can reduce to a hostname. */
  iconHost: string;
  enabled: boolean;
  onToggle: (enabled: boolean) => void;
}

/** AccountSwitches is the gear for a card's accounts, drawn only when it has one to switch. */
export function AccountSwitches({
  hue,
  accounts,
  onAll,
}: {
  hue: number;
  accounts: SwitchableAccount[];
  onAll: (enabled: boolean) => void;
}) {
  const { t } = useT();
  const menu = useContextMenu();
  const [listing, setListing] = useState(false);
  const anchor = useRef<HTMLSpanElement>(null);
  if (accounts.length === 0) return null;

  const label = t('accounts.cardActions');
  const title = t('accounts.enableTitle');
  return (
    <span ref={anchor} className="inline-flex">
      <IconBadge
        icon={<IconGear width={16} height={16} />}
        title={label}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={menu.anchor !== null}
        data-new="account-switches"
        onClick={(e) => menu.openAt(anchorBelow(e.currentTarget))}
      />
      {menu.anchor && (
        <ContextMenu
          anchor={menu.anchor}
          label={label}
          onClose={menu.close}
          groups={[
            { id: 'list', items: [{ id: 'list', label: title, onSelect: () => setListing(true) }] },
            {
              id: 'all',
              items: [
                {
                  id: 'enable',
                  label: t('accounts.enableAll'),
                  disabled: accounts.every((a) => a.enabled),
                  onSelect: () => onAll(true),
                },
                {
                  id: 'disable',
                  label: t('accounts.disableAll'),
                  disabled: accounts.every((a) => !a.enabled),
                  onSelect: () => onAll(false),
                },
              ],
            },
          ]}
        />
      )}
      {listing && (
        <AnchoredPanel anchor={anchor} title={title} hue={hue} narrow onClose={() => setListing(false)}>
          <ul className="flex flex-col gap-3">
            {accounts.map((a) => (
              <li key={a.key} className="flex items-center justify-between gap-4">
                <span
                  className={`inline-flex min-w-0 items-center gap-2 text-sm ${
                    a.enabled ? 'text-carbon-text' : 'text-carbon-textMuted'
                  }`}
                >
                  <HosterIcon host={a.iconHost} />
                  <span className="truncate">{a.label}</span>
                </span>
                <Toggle
                  hideLabel
                  label={t('accounts.enableAccount', { account: a.label })}
                  checked={a.enabled}
                  onChange={a.onToggle}
                  hue={hue}
                />
              </li>
            ))}
          </ul>
        </AnchoredPanel>
      )}
    </span>
  );
}
