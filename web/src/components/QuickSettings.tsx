// The quick controls, and the shell-bar widget that carries them.
//
// The two live in one file because they cannot be separated at runtime: the
// speed meter and the gear open the same panel, so they share one piece of open
// state, and the widget reads one task stream that both the figures and the
// meter are computed from. Split apart, either the two openers drift out of step
// or the shell opens a second websocket to say the same number twice.
//
// What is deliberately NOT here: any figure called "open connections". A backend
// reports a status, a size, a byte count and a speed (core.Update) and never how
// many sockets it holds, so a live connection count would have to be invented.
// The chunk spinner below is the configured number one download opens, it is
// labelled as chunks, and its bubble says exactly that.
import { useCallback, useEffect, useState } from 'react';
import { type QueueState, fetchQueue, setQueue } from '../lib/api';
import { type Controls, type ControlsPatch, fetchControls, saveControls } from '../lib/controls';
import { fmtRateValue, splitRate } from '../lib/format';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { useListView } from '../lib/listview';
import { useTasks } from '../lib/useTasks';
import { useToast } from '../lib/toast';
import { useUIState } from '../lib/uistate';
import { Button, Field, InfoBubble, Modal, NumberInput, Toggle } from './ui';
import { OverviewStrip, type StripScope } from './Counters';
import { SpeedMeter } from './SpeedGraph';
import { IconPause, IconPlay, IconSliders } from '../lib/icons';

/**
 * One spinner that saves when it is LEFT, never as it is typed.
 *
 * This is the whole reason it is a component rather than a bare NumberInput.
 * Every write here ends in App.ApplySettings, which ends in dispatchLocked — so
 * a save per keystroke re-runs the scheduler for "1", then "12", then "128",
 * three passes over the queue to reach a number the user was in the middle of
 * writing. Blur and Enter are the two ways a person says they are finished.
 */
function Spin({
  label,
  hint,
  value,
  min,
  max,
  onCommit,
}: {
  label: string;
  hint?: string;
  value: number;
  min: number;
  max?: number;
  onCommit: (n: number) => void | Promise<void>;
}) {
  const [draft, setDraft] = useState(value);
  // Held while the box has focus AND until a save has been answered. The second
  // half matters as much as the first: released at blur, the field would snap
  // back to the stored number for as long as the request takes and then jump to
  // the new one, so every edit would flicker through its own previous value.
  const [held, setHeld] = useState(false);

  // Otherwise the stored value wins whenever it changes underneath — the server
  // clamps what it was sent and answers with the truth, and a save that failed
  // has to leave the field showing what is actually configured.
  useEffect(() => {
    if (!held) setDraft(value);
  }, [value, held]);

  async function commit() {
    if (draft !== value) await onCommit(draft);
    setHeld(false);
  }

  return (
    <Field label={label} hint={hint}>
      <NumberInput
        value={draft}
        min={min}
        max={max}
        onValue={setDraft}
        onFocus={() => setHeld(true)}
        onBlur={() => void commit()}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
        }}
      />
    </Field>
  );
}

/**
 * QuickSettings is the panel: the three counts that decide how much is open at
 * once, the master switch, and whether the speed limit is in force.
 *
 * It is the app's one overlay treatment rather than a popover of its own. A
 * second anchored panel would be a second thing to position, dismiss and make
 * keyboard-safe, and GlimStone's rule about sameness is a rule about how many of
 * these exist, not about how they look.
 *
 * Everything is read when it opens and nothing is polled. The panel is on screen
 * for seconds, and a widget that subscribes to the settings for the length of a
 * glance is a subscription to maintain for no gain.
 */
export function QuickSettings({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const { toast } = useToast();
  const [cfg, setCfg] = useState<Controls | null>(null);
  const [queue, setQ] = useState<QueueState | null>(null);
  // The limit the switch puts back. Held in the remembered UI state rather than
  // in this component, because the panel is unmounted the moment it is closed —
  // a value kept here would mean switching the limit off and on again inside one
  // session silently lifted it for good.
  const [lastLimit, setLastLimit] = useUIState('quickSpeedLimit', 0);

  useEffect(() => {
    let live = true;
    void fetchControls().then(
      (c) => {
        if (live) setCfg(c);
      },
      (e) => {
        // Guarded like the success path: a panel somebody closed while the
        // request was out must not throw a toast at the page they went to.
        if (live) toast(t('list.failed', { error: message(e) }), 'fail');
      },
    );
    void fetchQueue().then(
      (q) => {
        if (live) setQ(q);
      },
      () => {
        /* the switch stays out of the panel; the spinners are unaffected */
      },
    );
    return () => {
      live = false;
    };
  }, [t, toast]);

  const patch = useCallback(
    async (p: ControlsPatch) => {
      try {
        // Adopted, not assumed: the server clamps the concurrency numbers on the
        // way to disk, so the field settles on what was stored rather than on
        // what was asked for.
        setCfg(await saveControls(p));
      } catch (e) {
        toast(t('list.failed', { error: message(e) }), 'fail');
      }
    },
    [t, toast],
  );

  async function toggleHalt() {
    if (!queue) return;
    try {
      setQ(await setQueue({ halted: !queue.halted }));
    } catch (e) {
      toast(t('list.failed', { error: message(e) }), 'fail');
    }
  }

  function toggleLimit(on: boolean) {
    if (!cfg) return;
    if (on) {
      void patch({ speedLimit: lastLimit });
      return;
    }
    // Remembered before it is cleared, and only ever a real limit: writing a 0
    // here would make the switch unable to put anything back.
    if (cfg.speedLimit > 0) setLastLimit(cfg.speedLimit);
    void patch({ speedLimit: 0 });
  }

  const shown = cfg?.speedLimit || lastLimit;
  const rate = splitRate(shown);

  return (
    <Modal title={t('quick.title')} onClose={onClose}>
      {/* The same switch and the same words as the bar's, on purpose: this panel
          is also reached from the speed meter, and a reading of how fast the
          queue is going that cannot stop it is a panel people close again. */}
      {queue && (
        <div className="flex flex-wrap items-center gap-3">
          <Button
            kind={queue.halted ? 'primary' : 'secondary'}
            icon={queue.halted ? <IconPlay width={16} height={16} /> : <IconPause width={16} height={16} />}
            onClick={toggleHalt}
            className="px-2.5 text-xs"
          >
            {queue.halted ? t('queue.start') : t('queue.stop')}
          </Button>
          {queue.halted && <span className="text-statusInfo text-[11px]">{t('queue.halted')}</span>}
        </div>
      )}

      {cfg && (
        <>
          {/* Read together because they multiply: two downloads on one host, each
              pulled over eight sockets, is sixteen connections to that host and
              no one of the three numbers says so on its own. */}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {/* No `max` on the two concurrency spinners, and that is not an
                omission: their bound lives in settings.sanitizeQueue and is not
                served, so a number typed here would be a copy of it that drifts
                the day it moves. The save answers with what was stored, and the
                field adopts that instead. */}
            <Spin
              label={t('settings.maxConcurrent')}
              value={cfg.maxConcurrent}
              min={1}
              onCommit={(n) => patch({ maxConcurrent: n })}
            />
            <Spin
              label={t('settings.maxPerHost')}
              value={cfg.maxPerHost}
              min={1}
              onCommit={(n) => patch({ maxPerHost: n })}
            />
            {/* This one does have a bound, because the server sends it: it is the
                engine's own, and a spinner offering more than connsFor will
                honour is a control lying about what saving it did. */}
            <Spin
              label={t('settings.chunks')}
              hint={t('quick.chunksHint')}
              value={cfg.chunks}
              min={0}
              max={cfg.maxChunks}
              onCommit={(n) => patch({ chunks: n })}
            />
          </div>

          {shown > 0 ? (
            <div className="flex flex-wrap items-center gap-3">
              <Toggle
                checked={cfg.speedLimit > 0}
                onChange={toggleLimit}
                label={t('settings.speedLimit')}
              />
              {/* dir="ltr" so the number and its unit stay one token in a
                  right-to-left locale. Muted while the switch is off: the value
                  is what would come back, not what is in force. */}
              <span
                dir="ltr"
                className={`glim-num text-xs ${cfg.speedLimit > 0 ? 'text-carbon-textSub' : 'text-carbon-textMuted'}`}
              >
                {fmtRateValue(rate.value)} {rate.unit}
              </span>
              <InfoBubble tip={t('quick.limitHint')} />
            </div>
          ) : (
            // A switch with nothing to switch is worse than a sentence: pressing
            // it would have to invent a limit nobody chose.
            <div className="flex items-center gap-1.5 text-xs text-carbon-textMuted">
              {t('quick.noLimit')}
              <InfoBubble tip={t('quick.noLimitHint')} />
            </div>
          )}
        </>
      )}
    </Modal>
  );
}

/**
 * ShellStrip is what mounts in the shell bar's widget slot: the figures, the
 * speed, and the way into the panel.
 *
 * It reads the stream for the instance the SHELL is scoped to, so on a peer's
 * download list the figures describe that peer. The panel does not follow: none
 * of its knobs is forwarded to a peer (only the task, link and queue routes
 * are), so a gear over somebody else's list would quietly tune this machine. The
 * bar's scope tag has already said whose list is on screen, which is why nothing
 * here repeats it.
 */
const SCOPES: StripScope[] = ['total', 'visible', 'selected'];

export function ShellStrip() {
  const { t } = useT();
  const { instance } = useInstanceScope();
  const tasks = useTasks(instance);
  const view = useListView();
  const [open, setOpen] = useState(false);
  const [storedScope, setScope] = useUIState<StripScope>('shellScope', 'total');
  const [includeDisabled, setIncludeDisabled] = useUIState('shellIncludeDisabled', false);

  // The remembered document is opaque JSON that another build, or a hand, may
  // have written: an id the strip does not know would leave every tab
  // unselected with nothing saying why.
  const scope: StripScope = SCOPES.includes(storedScope) ? storedScope : 'total';

  let speed = 0;
  for (const id in tasks) {
    if (tasks[id].status === 'running') speed += tasks[id].speed;
  }

  const local = instance === '';

  return (
    <>
      <OverviewStrip
        tasks={tasks}
        view={view}
        scope={scope}
        onScope={setScope}
        includeDisabled={includeDisabled}
        onIncludeDisabled={setIncludeDisabled}
      />

      <span className="flex items-center gap-1">
        <SpeedMeter value={speed} label={t('quick.title')} onOpen={local ? () => setOpen(true) : undefined} />
        {local && (
          <Button
            kind="ghost"
            icon={<IconSliders width={16} height={16} />}
            aria-label={t('quick.title')}
            title={t('quick.title')}
            onClick={() => setOpen(true)}
          />
        )}
      </span>

      {open && <QuickSettings onClose={() => setOpen(false)} />}
    </>
  );
}

/** The server's own sentence when there is one; these routes refuse with a reason. */
function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
