import { Link } from 'react-router-dom';
import { useT } from '../../lib/i18n';
import { Card, EmptyState, SectionTitle } from '../ui';
import type { Task } from '../../lib/api';

/**
 * RulesCard lists the rules that fired on a link while it was staged, each a
 * link into the rule editor.
 *
 * Rules have no id, so the chip carries the name the engine recorded, which is
 * the server's untranslated "rule 3" for an unnamed rule. A renamed or moved
 * rule can therefore miss; the (i) says so.
 */
export function RulesCard({ task, hue }: { task: Task; hue?: number }) {
  const { t } = useT();
  const rules = task.matchedRules ?? [];

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('detail.rulesHint')}>{t('detail.rules')}</SectionTitle>

      {rules.length === 0 ? (
        // Kept even when empty: "no rule touched this link" is an answer.
        <EmptyState nested title={t('detail.noRules')} />
      ) : (
        <div className="flex flex-wrap gap-2">
          {rules.map((name, i) => (
            <Link
              // Two rules can share a name.
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
