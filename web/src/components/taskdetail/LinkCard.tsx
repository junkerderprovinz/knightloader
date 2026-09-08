import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { fmtBytes } from '../../lib/format';
import { hostOf, useConnectionLabel } from '../columns';
import { ResolverBadge } from '../StatusPill';
import { Card, SectionTitle } from '../ui';
import { Fact } from './Fact';
import type { Task, TaskFileHead } from '../../lib/api';

/**
 * Where this download came from and what is carrying it: the address, the page
 * a crawl found it on, the hoster, the backend, the connection, the intake
 * path, the package, and how many bytes are on disk this second.
 *
 * The row tooltip (columns.tsx) already answers the first few of these on
 * hover, and it keeps doing so: the tooltip is the quick read while scanning a
 * list, and this card is the one that stays open, can be right-clicked, and
 * has room for the (i) that explains why the address and the hoster are not
 * the same question.
 */
export function LinkCard({
  task,
  base,
  head,
  hue,
}: {
  task: Task;
  base: string;
  /**
   * The answer to the HEAD probe, or null while it has not run or was not
   * worth running. It is passed in rather than fetched here on purpose: the
   * player card wants the same answer, and two cards each probing the same
   * route would double a request that costs the server a pair of symlink
   * resolutions and a stat. See TaskDetailPanel for the one place it is asked.
   */
  head: TaskFileHead | null;
  hue?: number;
}) {
  const { t } = useT();
  // The shared hook, not a second copy of the rule: a task's connection has to
  // resolve to the same words here that the column and the row tooltip already
  // show it as. Null means nothing has routed this task yet, which is "nobody
  // has decided", not "direct", and so it draws no row at all.
  const connection = useConnectionLabel(task, t, base);
  const host = hostOf(task);

  // The intake path reuses the collector's own words. An id this build has
  // never heard of keeps the raw value, exactly as FilteredLinks already does
  // for the same list: the taxonomy grows on the server, and a blank where a
  // new intake path should be is worse than an untranslated one.
  const originKey = `collector.filtered.origin.${task.origin ?? ''}` as TranslationKey;
  const origin = task.origin ? (originKey in en ? t(originKey) : task.origin) : t('detail.originUnknown');

  // Only once the probe has actually answered, and only when it answered yes.
  // A refusal is the player card's story to tell, with the right sentence for
  // the right status; a "0 B" here would be the wrong answer to a different
  // question.
  const onDisk = head?.ok ? fmtBytes(head.bytes) : '';

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      {/* No `hue` on the title: the position lives on the Card, and the badge
          inherits it from there. Passing it twice sets the variables a second
          time on an element that already had them. */}
      <SectionTitle>{t('detail.link')}</SectionTitle>

      <Fact label={t('detail.address')} hint={t('detail.addressHint')} value={task.url} ltr copy />
      <Fact label={t('columns.source')} value={task.source} ltr copy />
      <Fact label={t('columns.host')} value={host} ltr />
      <Fact label={t('columns.resolver')}>
        <ResolverBadge resolver={task.resolver} mode={task.mode} />
      </Fact>
      {connection && <Fact label={t('columns.connection')} value={connection.text} ltr />}
      <Fact label={t('detail.origin')} value={origin} />
      {/* An empty package is not a package called nothing: it is a link that no
          rule and no hand has grouped, and the ungrouped rows are a real place
          in the list rather than an absence. So this row is always drawn. */}
      <Fact label={t('detail.package')} value={task.package || t('detail.noPackage')} />
      <Fact label={t('detail.mirrorOf')} value={task.mirrorOf} ltr />
      <Fact label={t('detail.onDisk')} hint={t('detail.onDiskHint')} value={onDisk} />
    </Card>
  );
}
