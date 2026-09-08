import { useT } from '../lib/i18n';
import { InfoBubble } from './ui';

/**
 * "18 selected, 12 of them not visible."
 *
 * The second half of that sentence is the whole component. A selection survives
 * a search, a filter and a fold - deliberately, see lib/selectionReach.ts - so
 * the number beside it is regularly larger than what anybody can see, and Del
 * takes the whole of it. This says so before the press.
 *
 * It replaces the bare `{n} {t('select.count')}` span the two list pages already
 * draw rather than sitting beside it, because the addendum has to hang off that
 * exact number and a second free-standing count would read as a second
 * selection. `select.count` itself is untouched: it is the bare word
 * ("ausgewählt"), composed the same way at four call sites including the
 * Properties panel header, and turning it into a sentence would rewrite all four
 * in 42 languages.
 *
 * THE NUMBER IS A BUTTON, not a line of text with a control beside it: pressing
 * it drops the rows nobody can see and keeps the selection to what is on screen.
 * That makes the whole readout tab-reachable without the command surface having
 * to publish anything, and the explanation goes in the bubble, which is where
 * this app puts explanations.
 *
 * IT DRAWS NOTHING WHEN NOTHING IS HIDDEN. A permanent "0 of them not visible"
 * is furniture that trains people to stop reading the line, on the one line that
 * has to be read the day it says twelve.
 */
export function SelectionReach({
  total,
  hidden,
  mode,
  onReduce,
}: {
  /** The whole selection, which is what `select.count` counts. */
  total: number;
  /** How many of those the list is not drawing. Zero renders nothing at all. */
  hidden: number;
  /**
   * Which sentence the button carries. The two are NOT one string with a
   * different verb: on the list the press keeps the visible rows selected, and
   * in the removal dialog it narrows what is about to be deleted and touches no
   * selection at all. A shared label would be wrong in one of the two places,
   * and it would be wrong in the destructive one.
   */
  mode: 'select' | 'removal';
  onReduce: () => void;
}) {
  const { t } = useT();
  // In the dialog the count is already on screen, in the dialog's own words
  // ("18 downloads will be removed"), so this only ever adds the clause. On the
  // list there is no other count, so this owns both halves.
  const owns = mode === 'select';
  if (hidden <= 0) {
    if (!owns) return null;
    return (
      <span className="glim-num text-sm text-carbon-textSub">
        {total} {t('select.count')}
      </span>
    );
  }
  const label = mode === 'removal' ? t('select.reduceRemove') : t('select.reduceKeep');
  const aria = (mode === 'removal' ? t('select.reduceRemoveAria') : t('select.reduceKeepAria')).replace(
    '{n}',
    String(hidden),
  );
  return (
    <span className="flex items-center gap-1">
      <span className="glim-num text-sm text-carbon-textSub">
        {owns && `${total} ${t('select.count')}, `}
        {/* Inside the same span as the count and separated by a comma, because
            the two halves are one sentence: "18 selected, 12 of them not
            visible". A button on its own line would read as a second control
            about something else. */}
        <button
          type="button"
          title={label}
          aria-label={aria}
          className="rounded-[var(--radius-control)] underline decoration-dotted underline-offset-2
            transition-colors hover:text-carbon-text focus-visible:text-carbon-text"
          onClick={onReduce}
        >
          {t('select.hidden').replace('{n}', String(hidden))}
        </button>
      </span>
      <InfoBubble tip={t('select.hiddenTip')} />
    </span>
  );
}
