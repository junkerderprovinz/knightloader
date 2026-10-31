import { useState } from 'react';
import { Button } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { appAddress } from '../../../lib/basePath';
import { ModuleCard } from '../ModuleToggle';
import { Reading } from '../health/Reading';
import { IconCheckDrawn, IconCopy } from '../../../lib/icons';

/**
 * ScrapeCard switches the plain-text metrics route, which is off by default and
 * sits behind the usual authentication.
 */
export function ScrapeCard({ hue }: { hue: number }) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const [copies, setCopies] = useState(0);

  // Absolute, because it gets pasted into a collector on another machine, and
  // taken from the address the reader used since the server cannot know it.
  const address = `${appAddress()}/api/metrics`;

  return (
    <ModuleCard id="metrics" hue={hue} switchHue={0} hint={t('settings.health.scrapeHint')}>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <Reading label={t('settings.health.scrapeUrl')} value={address} />
        {/* navigator.clipboard is missing on a plain-HTTP LAN address. */}
        {'clipboard' in navigator && (
          <Button
            icon={copied ? <IconCheckDrawn /> : <IconCopy />}
            kind="secondary"
            className="px-2.5 text-xs"
            confirm={copies}
            onClick={async () => {
              await navigator.clipboard.writeText(address);
              setCopied(true);
              setCopies((n) => n + 1);
              setTimeout(() => setCopied(false), 1800);
            }}
          >
            {copied ? t('settings.health.scrapeCopied') : t('settings.health.scrapeCopy')}
          </Button>
        )}
      </div>
    </ModuleCard>
  );
}
