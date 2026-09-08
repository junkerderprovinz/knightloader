import { Link } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Card, EmptyState, SectionTitle } from '../ui';
import type { Task } from '../../lib/api';

/**
 * The rules that fired on this link while it was being staged, in the order
 * they fired, each one a way into the editor that owns it.
 *
 * The list has been on the wire since the rule engine shipped and has been
 * shown exactly once anywhere, as plain text in the collector's holding area.
 * Which is the wrong half: the holding area only ever shows the ONE rule that
 * refused a link, while a link that sailed through can still have been renamed,
 * repackaged and re-foldered by three of them, and nothing said so.
 *
 * A NAME IS NOT AN ID, and this card must not pretend otherwise. rules.Rule
 * has no id at all and its Name is omitempty, so the engine writes either the
 * trimmed name or the literal "rule 3" for an unnamed one. A rule renamed
 * since is therefore unfindable, and an unnamed one points at whatever is
 * third in the list today, which the editor's own move, duplicate and remove
 * reorder freely. Both of those are in the (i), and a click that misses gets a
 * sentence on the rules page rather than doing nothing.
 *
 * THE CHIP PRINTS THE SERVER'S OWN WORD, untranslated. "rule 3" is lowercase
 * English out of internal/rules, unconditionally, in every locale. The
 * catalogue's own settings.rules.unnamed reads "Rule {n}" with a capital, and
 * showing that instead would hand the user a handle that does not match the
 * thing it is a handle for.
 */
export function RulesCard({ task, hue }: { task: Task; hue?: number }) {
  const { t } = useT();
  const rules = task.matchedRules ?? [];

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('detail.rulesHint')}>{t('detail.rules')}</SectionTitle>

      {rules.length === 0 ? (
        // Kept as a real card even when it is empty, unlike the failure card
        // above. "No rule touched this link" is an answer somebody comes to
        // this panel for, and it is the answer most often wanted in the
        // collector, where a link that arrived wearing the wrong package name
        // is exactly the case where knowing that no rule was involved settles
        // it. `nested`, because this sits inside a Card already.
        <EmptyState nested title={t('detail.noRules')} />
      ) : (
        <div className="flex flex-wrap gap-2">
          {rules.map((name, i) => (
            <Link
              // The name is the whole address here, so two rules of the same
              // name give two chips that go to the same place; the index keeps
              // React's list stable without pretending the name is unique.
              key={`${name}-${i}`}
              to={`/settings/rules?rule=${encodeURIComponent(name)}`}
              title={t('detail.openRule')}
              dir="ltr"
              className="inline-flex h-8 shrink-0 items-center rounded-[var(--radius-control)] bg-carbon-surface2
                px-3 text-[11px] font-medium text-carbon-textSub transition duration-150
                hover:brightness-110 hover:text-carbon-text motion-safe:active:scale-[.98]"
            >
              {name}
            </Link>
          ))}
        </div>
      )}
    </Card>
  );
}
