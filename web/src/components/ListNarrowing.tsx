// What narrows a list and what is picked in it, under the totals of its page:
// how many rows the search and the filters leave, with the one click that
// shows them all again, the selection, and the saved views.
import type { ReactNode } from 'react';
import { useT } from '../lib/i18n';
import { IconClose } from '../lib/icons';
import { Button } from './ui';

export function ListNarrowing({
  shown,
  total,
  narrowed,
  onClear,
  selection,
  views,
}: {
  shown: number;
  total: number;
  narrowed: boolean;
  onClear: () => void;
  /** The selection's count and its ways out, while something is picked. */
  selection?: ReactNode;
  views: ReactNode;
}) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-2">
      {(narrowed || selection) && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
          {narrowed && (
            <span className="flex items-center gap-2">
              <span className="glim-num text-carbon-textSub">{t('search.shown', { n: shown, total })}</span>
              <Button
                kind="secondary"
                icon={<IconClose />}
                title={t('views.clearAll')}
                aria-label={t('views.clearAll')}
                onClick={onClear}
              />
            </span>
          )}
          {selection}
        </div>
      )}
      {views}
    </div>
  );
}
