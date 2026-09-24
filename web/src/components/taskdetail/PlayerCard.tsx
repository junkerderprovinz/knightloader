import { useEffect, useState } from 'react';
import { isLocalBase, taskFileURL, type Task, type TaskFileHead } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { reachable as taskFileReachable } from '../FileActions';
import { Button, Card, ErrorCard, SectionTitle } from '../ui';
import { playableAs } from './playable';

/**
 * PlayerCard plays a task's audio or video file straight off the instance that
 * fetched it. Each gate that blocks playing gets its own sentence in the (i),
 * and nothing is fetched before the button is pressed (preload="none").
 */
export function PlayerCard({
  task,
  base,
  head,
  hue,
}: {
  task: Task;
  base: string;
  /** The HEAD probe's answer, or null while it has not run. */
  head: TaskFileHead | null;
  hue?: number;
}) {
  const { t } = useT();
  const [playing, setPlaying] = useState(false);
  const [failed, setFailed] = useState('');

  // The panel has no key so that live task updates do not remount the <video>,
  // which means a new selection has to close the player here.
  useEffect(() => {
    setPlaying(false);
    setFailed('');
  }, [task.id, base]);

  const media = playableAs(task.name);
  if (!media) return null;

  // A peer is ruled out first: the federation proxy drops the Range header,
  // truncates at 32 MB and relabels the body as application/json.
  let why = '';
  if (!isLocalBase(base)) why = t('detail.playRemote');
  // Shares the file menu's rule, but a JD task's file lives on the sidecar's
  // disk while an unresolved link has no file at all, so the sentences differ.
  else if (!taskFileReachable(task))
    why = task.resolver === 'jd' ? t('detail.playNotLocal') : t('detail.playNoFile');
  else if (!media.supported) why = t('detail.playUnsupported');
  // 404 is the ordinary state of a download that has not started; 400 and 403
  // are the server declining, and 403 means a folder resolved outside the tree.
  else if (head && !head.ok)
    why = head.status === 404 ? t('detail.playNoFile') : t('detail.playRefused', { reason: String(head.status) });

  // A probe still in flight gets no sentence, only a disabled button.
  const ready = !why && !!head?.ok;
  // ServeContent measures a growing file when the stream opens, so seeking past
  // that point fails and a fragmented MP4 with a trailing index plays nothing.
  const partial = task.status !== 'done';
  const note = why || (partial ? t('detail.playPartial') : '');
  const src = taskFileURL(task.id, base);

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('detail.playHint')}>{t('detail.player')}</SectionTitle>

      {failed ? (
        <ErrorCard
          nested
          message={failed}
          retryLabel={t('detail.play')}
          retry={() => {
            setFailed('');
            setPlaying(true);
          }}
        />
      ) : playing ? (
        <div className="flex flex-col gap-3">
          {/* No `type` attribute: the server's allowlist decides the type. */}
          {media.kind === 'video' ? (
            <video
              src={src}
              controls
              preload="none"
              playsInline
              className="max-h-[60vh] w-full rounded-[var(--radius-control)] bg-black"
              onError={(e) => setFailed(t('detail.playFailed', { reason: mediaError(e.currentTarget) }))}
            />
          ) : (
            <audio
              src={src}
              controls
              preload="none"
              className="w-full"
              onError={(e) => setFailed(t('detail.playFailed', { reason: mediaError(e.currentTarget) }))}
            />
          )}
          <div className="flex items-center gap-2">
            <Button kind="ghost" hint={note || undefined} onClick={() => setPlaying(false)}>
              {t('detail.playClose')}
            </Button>
          </div>
        </div>
      ) : (
        // Disabled rather than hidden, so people learn that playing is possible.
        <div className="flex items-center gap-2">
          <Button disabled={!ready} hint={note || undefined} onClick={() => setPlaying(true)}>
            {t('detail.play')}
          </Button>
        </div>
      )}
    </Card>
  );
}

// MediaError.message is empty in most browsers, so the code stands in for it.
function mediaError(el: HTMLMediaElement): string {
  return el.error?.message || String(el.error?.code ?? 0);
}
