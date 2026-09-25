import { useState } from 'react';
import { Button, Card, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useFeatures } from '../context';
import { ModuleToggle } from '../ModuleToggle';
import { Reading } from './Reading';

/**
 * ScrapeCard switches the plain-text metrics route, which is off by default and
 * sits behind the usual authentication. The switch is the module's own, so it
 * cannot disagree with the Modules page.
 */
export function ScrapeCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { features } = useFeatures();
  const [copied, setCopied] = useState(false);

  const on = features.modules.find((m) => m.id === 'metrics')?.enabled ?? false;

  // Absolute, because it gets pasted into a collector on another machine, and
  // taken from the origin the reader used since the server cannot know it.
  const address = `${window.location.origin}/api/metrics`;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.module.metrics')}</SectionTitle>

      <ModuleToggle id="metrics" hue={0} hint={t('settings.health.scrapeHint')} />

      <div className="flex flex-wrap items-end justify-between gap-3">
        <Reading label={t('settings.health.scrapeUrl')} value={address} />
        {/* navigator.clipboard is missing on a plain-HTTP LAN address. */}
        {'clipboard' in navigator && (
          <Button
            kind="ghost"
            className="px-2.5 text-xs"
            onClick={async () => {
              await navigator.clipboard.writeText(address);
              setCopied(true);
              setTimeout(() => setCopied(false), 1800);
            }}
          >
            {copied ? t('settings.health.scrapeCopied') : t('settings.health.scrapeCopy')}
          </Button>
        )}
      </div>

      {/* While the switch is off the address answers 404. */}
      {!on && <span className="text-[11px] text-carbon-textMuted">{t('settings.health.scrapeOffHint')}</span>}
    </Card>
  );
}
