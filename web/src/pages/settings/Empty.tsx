import { EmptyState } from '../../components/ui';
import { useFeatures } from './context';
import { label, useTx } from './tx';

/**
 * EmptyPage stands in for a registered sub-page that has no controls yet. The
 * reason comes from the server's module registry, so the page follows when the
 * subsystem ships.
 */
export function EmptyPage({ id }: { id: string }) {
  const { tx } = useTx();
  const { features } = useFeatures();

  const mine = features.modules.filter((m) => m.page === id);
  const missing = mine.filter((m) => m.verdict !== 'shipped');

  if (missing.length > 0) {
    return (
      <div className="flex flex-col gap-3">
        {missing.map((m) => (
          <EmptyState
            key={m.id}
            title={label(tx, 'settings.module.', m.id)}
            hint={m.reason}
          />
        ))}
      </div>
    );
  }

  return <EmptyState title={tx('settings.empty')} hint={tx('settings.emptyHint')} />;
}
