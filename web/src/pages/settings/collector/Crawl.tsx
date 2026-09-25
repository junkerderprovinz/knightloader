import { Card, Field, NumberInput, SectionTitle, TextArea, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useDraft } from '../context';
import { ModuleToggle } from '../ModuleToggle';

// The page crawler: a pasted page is opened and the files it links to are
// collected instead of the page itself.

export function CrawlCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.module.crawler')}</SectionTitle>
      <ModuleToggle id="crawler" hue={0} hint={t('settings.crawl')} />

      {cfg.crawl && (
      <div className="flex flex-col gap-5">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {/* max is internal/crawler.MaxDepth. */}
          <Field label={t('settings.crawl.depth')} hint={t('settings.crawl.depthHint')}>
            <NumberInput value={cfg.crawlDepth} min={1} max={3} onValue={(v) => patch({ crawlDepth: v })} />
          </Field>
          {/* A depth of 1 is one page, so the page cap and the same-host rule
              only apply from 2. */}
          {cfg.crawlDepth >= 2 && (
          <Field label={t('settings.crawl.maxPages')} hint={t('settings.crawl.maxPagesHint')}>
            <NumberInput value={cfg.crawlMaxPages} min={1} max={200} onValue={(v) => patch({ crawlMaxPages: v })} />
          </Field>
          )}
        </div>

        {cfg.crawlDepth >= 2 && (
        <ToggleRow
          hue={1}
          checked={cfg.crawlSameHost}
          onChange={(v) => patch({ crawlSameHost: v })}
          label={t('settings.crawl.sameHost')}
          hint={t('settings.crawl.sameHostHint')}
        />
        )}

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={t('settings.crawl.include')} hint={t('settings.crawl.includeHint')}>
            <TextArea
              rows={3}
              spellCheck={false}
              value={(cfg.crawlInclude ?? []).join('\n')}
              onChange={(e) => patch({ crawlInclude: e.target.value.split('\n').filter((p) => p.trim() !== '') })}
            />
          </Field>
          <Field label={t('settings.crawl.exclude')} hint={t('settings.crawl.excludeHint')}>
            <TextArea
              rows={3}
              spellCheck={false}
              value={(cfg.crawlExclude ?? []).join('\n')}
              onChange={(e) => patch({ crawlExclude: e.target.value.split('\n').filter((p) => p.trim() !== '') })}
            />
          </Field>
        </div>
      </div>
      )}
    </Card>
  );
}
