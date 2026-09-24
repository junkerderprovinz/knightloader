import { Button, Card, InfoBubble, SectionTitle, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useQuietMode, useToast } from '../../../lib/toast';
import {
  NOTIFY_EVENTS,
  SYSTEM_SUPPORTED,
  showSystem,
  useNotifyChannels,
  type NotifyChannel,
} from '../../../lib/notify';

/**
 * The Benachrichtigungen card: quiet mode first, since it filters on top of
 * every choice below it, then one row per event and three places it can land.
 */

/** "Show nothing" rather than "off", so the choice reads back as a decision. */
const CHANNELS: { id: NotifyChannel; label: TranslationKey }[] = [
  { id: 'app', label: 'notifications.channel.app' },
  { id: 'system', label: 'notifications.channel.system' },
  { id: 'silent', label: 'notifications.channel.silent' },
];

export function NotificationsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const { channels, permission, choose, ask } = useNotifyChannels();
  const [quiet, setQuiet] = useQuietMode();

  // Empty while the permission is granted or was never asked for.
  const status = !SYSTEM_SUPPORTED
    ? t('notifications.systemUnavailable')
    : permission === 'denied'
      ? t('notifications.systemBlocked')
      : '';

  async function pick(kind: (typeof NOTIFY_EVENTS)[number]['kind'], next: NotifyChannel) {
    const outcome = await choose(kind, next);
    // Chrome's quieter messaging can resolve to 'default' without a prompt, so
    // the press would otherwise look like it did nothing.
    if (next === 'system' && outcome === 'default') toast(t('notifications.systemNotGranted'), 'info');
  }

  // A real press may ask for permission; without it the test falls back to the bubble.
  async function sendTest() {
    const outcome = await ask();
    if (outcome === 'granted') showSystem(t('notifications.title'), t('notifications.testBody'), 'test');
    else toast(t('notifications.testBody'), 'info');
  }

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('notifications.titleHint')}
        right={
          SYSTEM_SUPPORTED ? (
            <Button kind="secondary" onClick={() => void sendTest()}>
              {t('notifications.test')}
            </Button>
          ) : undefined
        }
      >
        {t('notifications.title')}
      </SectionTitle>

      <ToggleRow
        label={t('notifications.quiet')}
        hint={t('notifications.quietHint')}
        checked={quiet}
        onChange={setQuiet}
      />

      {status && <span className="text-[11px] text-carbon-textMuted">{status}</span>}

      {NOTIFY_EVENTS.map((ev) => {
        const label = t(ev.label);
        // The stored choice, not a clamped one, so a phone on plain HTTP does
        // not overwrite what the desktop set (lib/notify.ts).
        const active = channels[ev.kind] ?? ev.fallback;
        return (
          // A well strip sizes every segment to the longest label, so the row wraps.
          <div key={ev.kind} className="flex flex-wrap items-center justify-between gap-3">
            <span className="flex items-center gap-1.5 text-sm text-carbon-text">
              {label}
              <InfoBubble tip={t(ev.hint)} />
            </span>
            {/* Not in a Field, whose label would pass a click on the caption to
                the first segment. activateOnFocus={false} keeps arrow keys from
                selecting "System notification" and raising the permission
                prompt, which can only be refused once. */}
            <Tabs
              variant="well"
              size="sm"
              activateOnFocus={false}
              label={label}
              active={active}
              onSelect={(id) => void pick(ev.kind, id as NotifyChannel)}
              items={CHANNELS.map((c) => ({
                id: c.id,
                label: t(c.label),
                // Dimmed rather than removed; the status line says why.
                dim: c.id === 'system' && !SYSTEM_SUPPORTED,
              }))}
            />
          </div>
        );
      })}
    </Card>
  );
}
