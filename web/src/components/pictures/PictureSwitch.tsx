import { useT } from '../../lib/i18n';
import { useUIState } from '../../lib/uistate';
import { Tabs } from '../Tabs';

export type PictureView = 'picture' | 'list';

/**
 * PictureSwitch stands beside a picture that took the place of plain controls
 * and brings them back, so they stay one click away.
 */
export function PictureSwitch({ view, onView }: { view: PictureView; onView: (view: PictureView) => void }) {
  const { t } = useT();
  return (
    <Tabs
      variant="well"
      size="sm"
      inline
      label={t('picture.view')}
      active={view}
      onSelect={(id) => onView(id as PictureView)}
      items={[
        { id: 'picture', label: t('picture.view.picture') },
        { id: 'list', label: t('picture.view.list') },
      ]}
    />
  );
}

/** usePictureView remembers which of the two a card shows, under `field`. The picture comes first. */
export function usePictureView(field: string): [PictureView, (view: PictureView) => void] {
  return useUIState<PictureView>(field, 'picture');
}
