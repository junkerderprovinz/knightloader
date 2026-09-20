import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { fmtBytes } from '../../lib/format';
import { hostOf, useConnectionLabel } from '../columns';
import { ResolverBadge } from '../StatusPill';
import { Card, SectionTitle } from '../ui';
import { Fact } from './Fact';
import type { Task, TaskFileHead } from '../../lib/api';

/**
 * LinkCard shows where a download came from and what carries it: address,
 * source page, hoster, backend, connection, intake path, package and the bytes
 * on disk.
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
   * The HEAD probe's answer, or null before it has run. TaskDetailPanel asks
   * once and shares it with the player card.
   */
  head: TaskFileHead | null;
  hue?: number;
}) {
  const { t } = useT();
  // Null means nothing has routed the task yet, which is not "direct".
  const connection = useConnectionLabel(task, t, base);
  const host = hostOf(task);

  // An intake path this build does not know keeps its raw id, as FilteredLinks
  // does, since the taxonomy grows on the server.
  const originKey = `collector.filtered.origin.${task.origin ?? ''}` as TranslationKey;
  const origin = task.origin ? (originKey in en ? t(originKey) : task.origin) : t('detail.originUnknown');

  // A refused probe is the player card's to explain; "0 B" here would mislead.
  const onDisk = head?.ok ? fmtBytes(head.bytes) : '';

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      {/* The title inherits the hue from the Card. */}
      <SectionTitle>{t('detail.link')}</SectionTitle>

      <Fact label={t('detail.address')} hint={t('detail.addressHint')} value={task.url} ltr copy />
      <Fact label={t('columns.source')} value={task.source} ltr copy />
      <Fact label={t('columns.host')} value={host} ltr />
      <Fact label={t('columns.resolver')}>
        <ResolverBadge resolver={task.resolver} mode={task.mode} />
      </Fact>
      {connection && <Fact label={t('columns.connection')} value={connection.text} ltr />}
      <Fact label={t('detail.origin')} value={origin} />
      {/* Always drawn: the ungrouped rows are a real place in the list. */}
      <Fact label={t('detail.package')} value={task.package || t('detail.noPackage')} />
      <Fact label={t('detail.mirrorOf')} value={task.mirrorOf} ltr />
      <Fact label={t('detail.onDisk')} hint={t('detail.onDiskHint')} value={onDisk} />
    </Card>
  );
}
