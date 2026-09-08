import { Button, Card, InfoBubble, SectionTitle } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import {
  NOTIFY_EVENTS,
  SYSTEM_SUPPORTED,
  showSystem,
  useNotifyChannels,
  type NotifyChannel,
} from '../../../lib/notify';

/**
 * Benachrichtigungen: one row per event, three places it can land.
 *
 * Its own card, directly above Quiet mode rather than inside it. The two are
 * different questions and the reading order says so: this card is "what does
 * each event do", the one below it is "and here is the one switch that mutes
 * the harmless ones". Quiet mode also keeps a card of its own because its TITLE
 * is its decision (lib/toast.tsx's QuietModeToggle writes that down), and one
 * SectionTitle per card is the rule.
 *
 * The card takes its hue from the caller, the same way Stall.tsx does: the page
 * decides the order of its cards, and a badge sequence that jumps reads as a bug.
 */

/**
 * The three segments, in the order they are offered.
 *
 * "Show nothing" and not "off": it is an instruction somebody gave, not the
 * absence of one, and a row set to it must read back as a decision.
 */
const CHANNELS: { id: NotifyChannel; label: TranslationKey }[] = [
  { id: 'app', label: 'notifications.channel.app' },
  { id: 'system', label: 'notifications.channel.system' },
  { id: 'silent', label: 'notifications.channel.silent' },
];

export function NotificationsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const { channels, permission, choose, ask } = useNotifyChannels();

  // One line, and only when there is something true to say. Nothing at all
  // while the permission has never been asked for or is granted, so this is a
  // status reading and not a permanent fixture on the card - the same shape and
  // the same reasoning as the Click'n'Load detail line on this page.
  const status = !SYSTEM_SUPPORTED
    ? t('notifications.systemUnavailable')
    : permission === 'denied'
      ? t('notifications.systemBlocked')
      : '';

  async function pick(kind: (typeof NOTIFY_EVENTS)[number]['kind'], next: NotifyChannel) {
    const outcome = await choose(kind, next);
    // Only 'default' needs saying. 'denied' is already on the card as the status
    // line above, 'granted' speaks for itself the next time the event fires, and
    // 'default' is the silent one: Chrome's quieter messaging resolves it
    // without showing anybody a prompt, so without this the press would look
    // like it did nothing.
    if (next === 'system' && outcome === 'default') toast(t('notifications.systemNotGranted'), 'info');
  }

  // The test is a real press, so it is a legitimate place to ask - and it is the
  // only other one in this app besides the segments themselves. A browser that
  // will not grant answers with the bubble instead, so pressing it always does
  // something visible rather than failing in silence.
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

      {status && <span className="text-[11px] text-carbon-textMuted">{status}</span>}

      {NOTIFY_EVENTS.map((ev) => {
        const label = t(ev.label);
        // The STORED choice, not a clamped one. A phone on plain HTTP opening
        // this card has to show what the desktop set, or the next press here
        // would quietly overwrite it (see lib/notify.ts on the shared bucket).
        const active = channels[ev.kind] ?? ev.fallback;
        return (
          // flex-wrap, deliberately: a well strip sizes every segment to the
          // longest label in the set, so three segments plus a caption is a wide
          // row. Wrapping under its own caption at a narrow viewport is right;
          // pushing the card into a horizontal scroll is not.
          <div key={ev.kind} className="flex flex-wrap items-center justify-between gap-3">
            {/* The row's NAME plus a bubble, never the explanation as the label:
                every explanatory sentence in this app lives behind the (i). */}
            <span className="flex items-center gap-1.5 text-sm text-carbon-text">
              {label}
              <InfoBubble tip={t(ev.hint)} />
            </span>
            {/* NOT wrapped in a Field. Field is a <label>, and a label hands its
                clicks to the first control inside it - ui.tsx records the
                measured failure this caused with the corner picker, where
                clicking the caption selected the first segment. Here that would
                mean the caption itself asking for notification permission.

                activateOnFocus={false} is the other half of the same danger, and
                it is the whole reason this prop is passed: Tabs defaults it to
                true for select="one", so tabbing into the strip and pressing
                Right would select "System notification" and raise the browser's
                permission prompt - from a keystroke somebody meant as
                navigation, on a prompt that can only be refused once. */}
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
                // Dimmed rather than removed: a segment that vanishes teaches
                // nobody that the channel exists, and the status line above
                // already says why this browser cannot offer it.
                dim: c.id === 'system' && !SYSTEM_SUPPORTED,
              }))}
            />
          </div>
        );
      })}
    </Card>
  );
}
