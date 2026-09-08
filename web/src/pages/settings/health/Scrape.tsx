import { useState } from 'react';
import { Button, Card, SectionTitle, ToggleRow } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import { useFeatures } from '../context';
import { Reading } from './Reading';

/**
 * The one control on this page: whether a monitoring system may fetch the same
 * reading as plain text.
 *
 * IT SHIPS OFF, and that is the owner's decision rather than a default that
 * drifted. The item this page came from asked for a second OPEN endpoint and
 * did not get one: the route sits behind the same authentication as everything
 * else, and this switch is what makes it exist at all. Somebody who wants it
 * scraped turns it on and knows why they did.
 *
 * A ToggleRow and never a checkbox, and the explanation is behind the row's own
 * (i) rather than printed under it - a settings page whose every row carries
 * two lines of grey prose is a page nobody reads twice.
 *
 * THE SWITCH GOES THROUGH THE MODULE REGISTRY, not through the settings draft,
 * for the reason the registry exists: `enabled` is derived from live state on
 * every request, so the row here and the row on the Modules page cannot
 * disagree. Going through the draft would mean a switch that reads "on" as soon
 * as it is clicked and a door that only opens at the next save.
 *
 * The card is drawn even when the server has no metrics module at all - an
 * older build, or one where the row was dropped. The address and the
 * explanation are still worth reading, and a switch is simply not offered for
 * something whose state nothing can report. Same shape as the Click'n'Load row
 * on the General tab.
 */
export function ScrapeCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { features, toggle } = useFeatures();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  const metrics = features.modules.find((m) => m.id === 'metrics');
  const on = metrics?.enabled ?? false;

  // The absolute address, because this is meant to be pasted into somebody
  // else's configuration file on another machine. A bare "/api/metrics" is
  // exactly the string that gets pasted into a Prometheus target and then does
  // not work. window.location.origin is the address the reader actually reached
  // this instance on, which is the one a collector on their network can use -
  // and it is deliberately not built from anything the server sent, because the
  // server does not know which of its names or ports somebody came in through.
  const address = `${window.location.origin}/api/metrics`;

  async function onSwitch(next: boolean) {
    setBusy(true);
    try {
      await toggle('metrics', next);
    } catch (e) {
      // The server refuses a switch that cannot do anything, and the refusal
      // names the reason. Swallowing it would leave a control that looks like
      // it worked, which is the failure the whole registry exists to prevent.
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
        {/* Guarded, because navigator.clipboard is absent on a plain-HTTP LAN
            address - which is the ordinary way this app is reached. A button
            that threw on every press would be worse than no button; the address
            beside it can always be selected by hand. */}
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

      {/* Said beside the address rather than left to be discovered: while the
          switch is off that address answers 404, exactly like an endpoint this
          build does not have. Somebody who pastes it into a collector first and
          reads the switch second would otherwise spend the evening on their
          network. */}
      {!on && <span className="text-[11px] text-carbon-textMuted">{t('settings.health.scrapeOffHint')}</span>}
    </Card>
  );
}
