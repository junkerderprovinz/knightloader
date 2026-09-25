import { Button, Modal } from './ui';
import { IconClose } from '../lib/icons';
import { useT } from '../lib/i18n';

/**
 * CoffeeDialog is Buy Me a Coffee's widget in a house window, so a donor pays
 * without leaving the app (GlimStone's reference/react/CoffeeDialog). The
 * widget page is the one BMAC page that allows framing, and the amount, the
 * message and the payment all happen inside it. The window is mounted only
 * while it is open, so nothing from BMAC loads before somebody asks for it.
 */
export function CoffeeDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  return (
    <Modal
      tall
      title={t('settings.about.coffeeButton')}
      hint={t('settings.about.coffeeIntro')}
      onClose={onClose}
      footer={
        <Button
          kind="primary"
          labelled
          icon={<IconClose width={16} height={16} />}
          title={t('common.close')}
          onClick={onClose}
        />
      }
    >
      <div className="flex min-h-0 flex-1 rounded-[var(--radius-card)] bg-carbon-surface2 p-2">
        {/* White behind the frame, so the first paint is not a dark hole on
            the dark theme; BMAC's page is light either way. */}
        <iframe
          src={WIDGET_URL}
          title={t('settings.about.coffeeButton')}
          allow="payment"
          className="min-h-0 w-full flex-1 rounded-[var(--radius-control)] border-0 bg-white"
        />
      </div>
    </Modal>
  );
}

/** The widget page for the coffee handle in README.md's donate row. */
const WIDGET_URL = 'https://buymeacoffee.com/widget/page/junkerderprovinz?color=%23FFDD00';
