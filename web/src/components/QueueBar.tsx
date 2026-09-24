import { useCallback, useEffect, useState } from 'react';
import { type QueueState, type StopCost, fetchQueue, fetchStopCost, setQueue, stopAll } from '../lib/api';
import { useDialogMute } from '../lib/dialogmute';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { Button, Modal } from './ui';
import { fmtBytes } from '../lib/format';
import { IconClose, IconPause, IconPlay, IconStop } from '../lib/icons';

const RETRY_MS = 5000;

/**
 * useQueueControl holds the queue's halted state and its verbs, shared by
 * QueueBar and the downloads commands. /api/queue is forwarded to peers, so
 * `base` may name one.
 */
export function useQueueControl(base: string, instance: string) {
  const [queue, setQ] = useState<QueueState | null>(null);
  // Bumped after a failed load. The shell mounts this once per session, so
  // without a retry a dropped request at boot would hide the controls.
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    let retry = 0;
    fetchQueue(base)
      .then((q) => {
        if (live) setQ(q);
      })
      .catch(() => {
        // The last known state is kept.
        if (live) retry = window.setTimeout(() => setAttempt((n) => n + 1), RETRY_MS);
      });
    return () => {
      live = false;
      clearTimeout(retry);
    };
  }, [base, instance, attempt]);

  const toggle = useCallback(async () => {
    if (!queue) return;
    setQ(await setQueue({ halted: !queue.halted }, base));
  }, [queue, base]);

  // For separate Play and Pause buttons, as JD draws them.
  const setHalted = useCallback(
    async (halted: boolean) => {
      setQ(await setQueue({ halted }, base));
    },
    [base],
  );

  // The hard stop (app.StopAll) interrupts running transfers, unlike halting.
  const stop = useCallback(async () => {
    const r = await stopAll(base);
    setQ(r.queue);
    return r;
  }, [base]);

  return { queue, toggle, setHalted, stop };
}

/**
 * QueueBar holds Play, Pause and the hard Stop in the shell bar. Halting only
 * stops new dispatch and lets running downloads finish. It takes no `base`:
 * the scope comes from lib/instance.tsx, the same value the page reads.
 */
export function QueueBar() {
  const { t } = useT();
  const { instance, base } = useInstanceScope();
  const { queue, setHalted, stop } = useQueueControl(base, instance);
  // What a hard stop would cost right now (app.StopCost), fetched on each press.
  const [stopCost, setStopCost] = useState<StopCost | null>(null);
  const [stopping, setStopping] = useState(false);
  const dialogs = useDialogMute();

  async function confirmStop() {
    setStopping(true);
    try {
      await stop();
      setStopCost(null);
    } finally {
      setStopping(false);
    }
  }

  if (!queue) return null;

  // Squares at the button height in every label mode, so the shell bar stays
  // one row high; the name is in the tooltip. The bar lays them out.
  return (
    <>
      {/* `secondary` rather than `ghost` for the inactive buttons, since a
          disabled ghost button has no fill and reads as gone. Stop asks first
          through the cost dialog. */}
      <Button
        kind={queue.halted ? 'primary' : 'secondary'}
        icon={<IconPlay />}
        onClick={() => void setHalted(false)}
        disabled={!queue.halted}
        title={t('queue.play')}
      />
      <Button
        kind={!queue.halted ? 'primary' : 'secondary'}
        icon={<IconPause />}
        onClick={() => void setHalted(true)}
        disabled={queue.halted}
        title={t('queue.pause')}
      />
      <Button
        kind="secondary"
        icon={<IconStop />}
        // With the dialog muted, the stop happens on the press.
        onClick={() => {
          if (dialogs.isMuted('hardStop')) {
            void stop();
            return;
          }
          void fetchStopCost(base).then(setStopCost);
        }}
        disabled={queue.running === 0}
        title={t('queue.hardStop')}
      />

      {stopCost && (
        <Modal
          title={t('queue.hardStopConfirmTitle')}
          mute="hardStop"
          onClose={() => (stopping ? undefined : setStopCost(null))}
          footer={
            // Both neutral, so the sentence decides; `secondary` so they stay
            // visible while disabled.
            <>
              <span className="flex-1" />
              <Button
                kind="secondary"
                labelled
                icon={<IconClose />}
                title={t('queue.hardStopConfirmCancel')}
                onClick={() => setStopCost(null)}
                disabled={stopping}
              />
              <Button kind="secondary" disabled={stopping} onClick={() => void confirmStop()}>
                {stopping ? t('settings.system.acting') : t('queue.hardStopConfirmProceed')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">
            {t('queue.hardStopConfirmBody', {
              n: stopCost.running,
              detail:
                stopCost.losing.length > 0
                  ? t('queue.hardStopConfirmLoss', { bytes: fmtBytes(stopCost.bytes) })
                  : t('queue.hardStopConfirmSafe'),
            })}
          </p>
          {stopCost.unknown > 0 && (
            <p className="mt-2 text-xs text-carbon-textMuted">{t('queue.hardStopConfirmUnknown', { n: stopCost.unknown })}</p>
          )}
        </Modal>
      )}
    </>
  );
}
