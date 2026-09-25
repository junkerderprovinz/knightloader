import { useT } from '../lib/i18n';
import { InfoBubble, useTooltip } from './ui';

/**
 * SelectionReach reads "18 selected, 12 of them not visible". A selection
 * survives search, filters and folds, so Del can reach rows nobody sees; the
 * hidden count is a button that drops them.
 */
export function SelectionReach({
  total,
  hidden,
  mode,
  onReduce,
}: {
  total: number;
  /** How many selected rows the list is not drawing. */
  hidden: number;
  /**
   * On the list the press keeps the visible rows selected; in the removal
   * dialog it narrows what is deleted and leaves the selection alone.
   */
  mode: 'select' | 'removal';
  onReduce: () => void;
}) {
  const { t } = useT();
  const label = mode === 'removal' ? t('select.reduceRemove') : t('select.reduceKeep');
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  // The removal dialog prints its own count, so there this adds only the clause.
  const owns = mode === 'select';
  if (hidden <= 0) {
    if (!owns) return null;
    return (
      <span className="glim-num text-sm text-carbon-textSub">
        {total} {t('select.count')}
      </span>
    );
  }
  const aria = (mode === 'removal' ? t('select.reduceRemoveAria') : t('select.reduceKeepAria')).replace(
    '{n}',
    String(hidden),
  );
  return (
    <span className="flex items-center gap-1">
      <span className="glim-num text-sm text-carbon-textSub">
        {owns && `${total} ${t('select.count')}, `}
        <button
          type="button"
          {...tipHoverProps}
          aria-label={aria}
          className="rounded-[var(--radius-control)] bg-carbon-surface2 px-1.5 py-0.5 transition-colors
            hover:bg-carbon-surface3 hover:text-carbon-text focus-visible:text-carbon-text"
          onClick={onReduce}
        >
          {t('select.hidden', { n: hidden })}
        </button>
        {tip.node}
      </span>
      <InfoBubble tip={t('select.hiddenTip')} />
    </span>
  );
}
