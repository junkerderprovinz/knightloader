// The quick-settings panel and the shell-bar widget that opens it, which share
// one piece of open state. There is no live connection count, because backends
// do not report their sockets; the chunk spinner is the configured number per
// download.
import { useCallback, useEffect, useState } from 'react';
import { type Controls, type ControlsPatch, fetchControls, saveControls } from '../lib/controls';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { useTasks } from '../lib/useTasks';
import { SpeedLimitField } from './QueueBar';
import { useToast } from '../lib/toast';
import { Button, Field, Modal, NumberInput } from './ui';
import { SpeedMeter } from './SpeedGraph';
import { VolumeMeter } from './VolumeMeter';
import { IconMenu } from '../lib/icons';

/**
 * Spin is a number field that saves on blur or Enter, not per keystroke,
 * because every save re-runs the scheduler.
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
  // Held while focused and until the save answers, or the field would flicker
  // back to the old value during the request.
  const [held, setHeld] = useState(false);

  // Otherwise the stored value wins, since the server clamps and a save can fail.
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
 * QuickSettings is a modal with the per-instance concurrency counts, read once
 * when it opens. Play, pause and the speed limit live in QueueBar.
 */
export function QuickSettings({ onClose }: { onClose: () => void }) {
  const { t } = useT();
  const { toast } = useToast();
  const [cfg, setCfg] = useState<Controls | null>(null);

  useEffect(() => {
    let live = true;
    void fetchControls().then(
      (c) => {
        if (live) setCfg(c);
      },
      (e) => {
        if (live) toast(t('list.failed', { error: message(e) }), 'fail');
      },
    );
    return () => {
      live = false;
    };
  }, [t, toast]);

  const patch = useCallback(
    async (p: ControlsPatch) => {
      try {
        // The server clamps, so the answer is adopted.
        setCfg(await saveControls(p));
      } catch (e) {
        toast(t('list.failed', { error: message(e) }), 'fail');
      }
    },
    [t, toast],
  );

  return (
    <Modal title={t('quick.title')} onClose={onClose}>
      {cfg && (
        <div className="flex flex-col gap-4">
          {/* No `max`: the bound lives in settings.sanitizeQueue and is not
              served, so the field adopts whatever the save stored. */}
          <Spin
            label={t('settings.maxConcurrent')}
            hint={t('settings.maxConcurrentHint')}
            value={cfg.maxConcurrent}
            min={1}
            onCommit={(n) => patch({ maxConcurrent: n })}
          />
          <Spin
            label={t('settings.maxPerHost')}
            hint={t('settings.maxPerHostHint')}
            value={cfg.maxPerHost}
            min={1}
            onCommit={(n) => patch({ maxPerHost: n })}
          />
          {/* The server sends this bound, the most connsFor will honour. */}
          <Spin
            label={t('settings.chunks')}
            hint={t('quick.chunksHint')}
            value={cfg.chunks}
            min={0}
            max={cfg.maxChunks}
            onCommit={(n) => patch({ chunks: n })}
          />
        </div>
      )}
    </Modal>
  );
}

/**
 * ShellStrip fills the shell bar's widget slot with the speed curve and the
 * way into quick settings. The speed follows the shell's instance scope; the
 * controls show for the local instance only, because peers forward no settings
 * routes.
 */
export function ShellStrip() {
  const { t } = useT();
  const { instance } = useInstanceScope();
  const tasks = useTasks(instance);
  const [open, setOpen] = useState(false);

  let speed = 0;
  for (const id in tasks) {
    if (tasks[id].status === 'running') speed += tasks[id].speed;
  }

  const local = instance === '';

  return (
    <>
      {/* items-stretch so the curve fills the card's height, and flex-1 so it
          takes the width. No h-full: a percentage height would take the item
          out of the stretch against an indefinite parent. */}
      <span className="flex flex-1 items-stretch gap-2">
        {/* The speed history is seeded only for the local instance, since
            /api/stats/speed is not forwarded; a peer starts live-only. */}
        <SpeedMeter value={speed} instance={instance} />
        {local && (
          // A fixed-width column, so the stacked controls share an edge.
          <span className="flex w-44 shrink-0 flex-col justify-center gap-2">
            <Button
              kind="secondary"
              className="w-full justify-center"
              icon={<IconMenu width={16} height={16} />}
              title={t('quick.title')}
              onClick={() => setOpen(true)}
            >
              {t('quick.title')}
            </Button>
            <SpeedLimitField />
            <VolumeMeter />
          </span>
        )}
      </span>

      {open && <QuickSettings onClose={() => setOpen(false)} />}
    </>
  );
}

// These routes refuse with a reason, so the server's sentence is shown.
function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
