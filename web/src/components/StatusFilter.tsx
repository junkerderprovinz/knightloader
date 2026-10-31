// What the ▾ in a list's Status head opens: the quick filters, each with how
// many rows it matches. Two on at once show both kinds, since the states
// exclude each other (matchesQuickFilters).
import { useT } from '../lib/i18n';
import { IconClose } from '../lib/icons';
import type { QuickFilter, QuickFilterId } from './ListToolbar';
import { Tabs } from './Tabs';
import { IconBadge } from './ui';

export function StatusFilter({
  filters,
  active,
  onToggle,
  onClear,
}: {
  filters: { f: QuickFilter; n: number }[];
  active: ReadonlySet<QuickFilterId>;
  onToggle: (id: QuickFilterId) => void;
  onClear: () => void;
}) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <span className="text-subline text-carbon-textMuted">{t('filter.label')}</span>
        <span className="flex-1" />
        <IconBadge
          labelled
          icon={<IconClose width={16} height={16} />}
          title={t('filter.clear')}
          aria-label={t('filter.clear')}
          disabled={active.size === 0}
          onClick={onClear}
        />
      </div>
      <Tabs
        inline
        select="many"
        size="sm"
        label={t('filter.label')}
        active={active}
        onSelect={(id) => onToggle(id as QuickFilterId)}
        items={filters.map(({ f, n }) => ({ id: f.id, label: t(f.label), badge: n }))}
      />
    </div>
  );
}
