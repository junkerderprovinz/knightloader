import { useState } from 'react';
import { Button, Card, SectionTitle, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import { useFeatures } from '../context';
import { Reading } from './Reading';

/**
 * ScrapeCard switches the plain-text metrics route, which is off by default and
 * sits behind the usual authentication. The switch goes through the module
 * registry rather than the draft, so it cannot disagree with the Modules page,
 * and it is left out when the server has no metrics module.
 */
export function ScrapeCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { features, toggle } = useFeatures();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  const metrics = features.modules.find((m) => m.id === 'metrics');
  const on = metrics?.enabled ?? false;

  // Absolute, because it gets pasted into a collector on another machine, and
  // taken from the origin the reader used since the server cannot know it.
  const address = `${window.location.origin}/api/metrics`;

  async function onSwitch(next: boolean) {
    setBusy(true);
    try {
      await toggle('metrics', next);
    } catch (e) {
      // The server refuses a switch that cannot do anything and names the reason.
      toast(t('settings.modules.switchFailed', { reason: String(e).replace(/^Error:\s*/, '') }), 'fail');
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle>{t('settings.health.scrape')}</SectionTitle>

      {metrics && (
        <ToggleRow
          hue={0}
          label={t('settings.health.scrapeSwitch')}
          hint={t('settings.health.scrapeHint')}
          checked={on}
          disabled={busy || metrics.switch === 'none'}
          onChange={(next) => void onSwitch(next)}
        />
      )}

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
