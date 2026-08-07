import { EmptyState } from '../../components/ui';
import { useFeatures } from './context';
import { label, useTx } from './tx';

/**
 * A sub-page that is registered and has no controls yet.
 *
 * Registered rather than absent on purpose: a later wave then fills a page that
 * already exists, at an address people may already have bookmarked, instead of
 * inventing one and re-deciding its name and its place in the rail. An absent
 * page also has no way to explain itself — and for Captcha the explanation is
 * the whole content, because nothing in this build produces a challenge at all.
 *
 * Which is why the text comes out of the module registry. The page does not
 * write its own excuse: if the reason changes because the subsystem landed, the
 * server changes and this page follows.
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
