import { useEffect, useState } from 'react';
import { isLocalBase, taskFileURL, type Task, type TaskFileHead } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { reachable as taskFileReachable } from '../FileActions';
import { Button, Card, ErrorCard, InfoBubble, SectionTitle } from '../ui';
import { playableAs } from './playable';

/**
 * Playing a finished (or half-finished) file straight off the instance that
 * fetched it, without downloading a second copy to find out what is in it.
 *
 * Nothing here is new on the server: GET /api/tasks/{id}/file already answers
 * Range, already sets Accept-Ranges through http.ServeContent, and already
 * decides the content type from its own extension allowlist. What was missing
 * was a way to reach it that was not "open it in a tab and let the browser
 * guess".
 *
 * FOUR GATES, EACH WITH ITS OWN SENTENCE, because folding them into one
 * "cannot play" would hide the only useful part. They are, in order: is this a
 * file this app serves as audio or video at all, is `base` this instance, is
 * the file on this machine's disk, does this browser take the format, and did
 * the route say yes when asked. The first one draws no card at all; the rest
 * disable the button and write the reason into the (i) beside it, the same way
 * the download settings' locked counter does.
 *
 * NOTHING IS FETCHED UNTIL THE BUTTON IS PRESSED. The element does not exist
 * before that, and when it does exist it carries preload="none". A panel that
 * opened a connection to a 4 GB film because somebody double-clicked its row
 * would be a panel people learn not to open. There is no autoplay, no
 * muted-autoplay trick, no picture-in-picture request and no fullscreen call:
 * the controls are the whole of the interface.
 */
export function PlayerCard({
  task,
  base,
  head,
  hue,
}: {
  task: Task;
  base: string;
  /** The HEAD probe's answer, or null while it has not run. Asked once in
   *  TaskDetailPanel and shared with the link card; see the note there on why
   *  it is not asked here. */
  head: TaskFileHead | null;
  hue?: number;
}) {
  const { t } = useT();
  const [playing, setPlaying] = useState(false);
  const [failed, setFailed] = useState('');

  // The panel deliberately carries no key, so that a task object replaced by
  // every WebSocket tick reconciles in place instead of tearing the <video>
  // down a second after it opened. The cost of that is this effect: moving the
  // selection to a different row keeps this component mounted, and without the
  // reset the next task would arrive with a player already open on it that
  // nobody pressed for.
  useEffect(() => {
    setPlaying(false);
    setFailed('');
  }, [task.id, base]);

  // Not a media file at all: no card, no dead button, no sentence. An archive
  // or an ISO has nothing to say here, and most of a download list is exactly
  // that.
  const media = playableAs(task.name);
  if (!media) return null;

  // The order matters, because each answer makes the next question pointless.
  //
  // The federation check is first and is not a nicety: the proxy forwards
  // anything under "tasks/", carries no request headers at all (so no Range
  // ever reaches the peer), reads at most 32 MB of the answer into memory and
  // relabels whatever comes back as application/json. A player pointed at that
  // gets a truncated body with a lying content type, which is worse than no
  // player, and it is why nothing that streams bytes may run against a peer.
  let why = '';
  if (!isLocalBase(base)) why = t('detail.playRemote');
  // The same rule the file menu already gates on, imported rather than
  // rewritten, so the player and the menu can never disagree about which files
  // are reachable. Only the WORDING is decided here: that one rule folds two
  // different situations together, a task the JD sidecar fetched (whose file is
  // on that process's disk) and a link that has never resolved a name and so
  // has no file anywhere yet. One sentence for both would blame JDownloader for
  // a link that is simply still sitting in the collector.
  else if (!taskFileReachable(task))
    why = task.resolver === 'jd' ? t('detail.playNotLocal') : t('detail.playNoFile');
  else if (!media.supported) why = t('detail.playUnsupported');
  // The route refuses for three different reasons and they are not
  // interchangeable: 404 is "nothing on disk yet", which is the ordinary state
  // of a download that has not started and not a fault at all; 400 and 403
  // mean the server declined, and 403 in particular means a stored folder
  // resolved outside the download tree, which is worth showing as itself.
  else if (head && !head.ok)
    why = head.status === 404 ? t('detail.playNoFile') : t('detail.playRefused', { reason: String(head.status) });

  // A probe still in flight is not a refusal, so it gets no sentence: the
  // button is simply not pressable for the moment it takes to answer.
  const ready = !why && !!head?.ok;
  // routes_files.go serves a growing file with a zero modtime so it is never
  // answered from cache, and ServeContent measures the length by seeking the
  // handle at request time. So what plays is the bytes that had arrived when
  // the element opened the stream: seeking past them does not work, a tail
  // that lands afterwards is not picked up, and a fragmented MP4 whose index
  // sits at the end plays nothing at all until the last byte is there.
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
          {/* No `type` attribute anywhere on these: the server decides what it
              is serving from its own allowlist, and a hint from this side that
              disagreed with the response would be the client overruling a
              security boundary. */}
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
            <Button kind="ghost" onClick={() => setPlaying(false)}>
              {t('detail.playClose')}
            </Button>
            {note && <InfoBubble tip={note} label={t('detail.play')} />}
          </div>
        </div>
      ) : (
        // Dimmed and locked rather than hidden, with the reason behind the (i)
        // beside it: a button that vanishes teaches nobody that playing off the
        // instance is possible at all, and the sentence is the whole of what
        // somebody needs in order to act on it.
        <div className="flex items-center gap-2">
          <Button disabled={!ready} onClick={() => setPlaying(true)}>
            {t('detail.play')}
          </Button>
          {note && <InfoBubble tip={note} label={t('detail.play')} />}
        </div>
      )}
    </Card>
  );
}

/**
 * What the element says went wrong, in its own words where it has any.
 *
 * MediaError.message is empty in more browsers than not, so the numeric code
 * stands in rather than an empty parenthesis: it is not a sentence, but it is
 * something that can be looked up, which "Playback stopped: " on its own is
 * not.
 */
function mediaError(el: HTMLMediaElement): string {
  return el.error?.message || String(el.error?.code ?? 0);
}
