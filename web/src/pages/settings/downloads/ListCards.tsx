import { Card, SectionTitle, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { setListCards } from '../../../lib/listCards';
import { useDraft } from '../context';

/**
 * ListCardsCard switches the Seeding and Finished cards of the Downloads page.
 * The app reads the same two settings through /api/appearance.
 */
export function ListCardsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <Card hue={hue} className="flex flex-col gap-3">
      <SectionTitle>{t('downloads.listTitle')}</SectionTitle>
      <ToggleRow
        hue={0}
        checked={cfg.seedingCard}
        onChange={(v) => {
          patch({ seedingCard: v });
          // The Downloads page follows at once instead of after the autosave.
          setListCards({ seeding: v });
        }}
        label={t('settings.downloads.seedingCard')}
        hint={t('settings.downloads.seedingCardHint')}
      />
      <ToggleRow
        hue={1}
        checked={cfg.finishedCard}
        onChange={(v) => {
          patch({ finishedCard: v });
          setListCards({ finished: v });
        }}
        label={t('settings.downloads.finishedCard')}
        hint={t('settings.downloads.finishedCardHint')}
      />
    </Card>
  );
}
