import { Button, Modal } from './ui';
import { IconClose } from '../lib/icons';
import { COFFEE_WIDGET } from '../lib/donate';
import { useT } from '../lib/i18n';

/**
 * CoffeeDialog is Buy Me a Coffee's own widget inside a window of the app, so
 * a donor pays without leaving it. The look inside the frame is BMAC's, card
 * and wallet payments included.
 */
export function CoffeeDialog({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  return (
    // As tall as the screen allows less a margin: BMAC's payment step runs to
    // about 1200px, and every pixel here is scrolling a donor is spared.
    <Modal
      title="Buy Me a Coffee"
      height="screen"
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
      <p className="text-sm text-carbon-textSub">{t('settings.about.coffeeIntro')}</p>
      <div className="flex min-h-0 flex-1 rounded-[var(--radius-card)] bg-carbon-surface2 p-2">
        {/* White behind the frame, so its first paint is not a dark hole on
            the dark theme; BMAC's page is light either way. */}
        <iframe
          src={COFFEE_WIDGET}
          title="Buy Me a Coffee"
          allow="payment"
          className="min-h-0 w-full flex-1 rounded-[var(--radius-control)] border-0 bg-white"
        />
      </div>
    </Modal>
  );
}
