// The global unpacking watcher: the source the two extraction rows of the
// notification matrix (lib/notify.ts) stand over.
//
// Without this hook those two rows would be controls over events that arrive
// nowhere. extraction-failed LOOKS built - Archives.tsx has three toast call
// sites carrying that kind - but all three fire when the user's own start or
// abort request fails, never when a job itself fails; and useExtractJobs is
// mounted per page, so it stops watching the moment somebody leaves Downloads.
// extraction-done has no call site at all. So this is mounted once in Layout,
// beside useCompletionToasts, and watches the stream for the whole session.
//
// Two rules, both taken from the download watcher it sits next to:
//
//  - TRANSITION ONLY. internal/app/app_extract.go broadcasts a full snapshot of
//    the job as it runs, several times over. Firing on `status === 'done'`
//    rather than on the change INTO 'done' is one notification every couple of
//    seconds for a large set. A job first seen already finished is skipped for
//    the same reason Layout.tsx skips its initial snapshot: nothing transitioned
//    while anybody was looking.
//  - 'cancelled' NEVER NOTIFIES. It is the user's own abort button, and telling
//    somebody their own press "failed" is worse than saying nothing.
import { useEffect, useRef } from 'react';
import { connectWS, type ExtractJob } from './api';
import { useT } from './i18n';
import { useToast } from './toast';

export function useExtractionToasts() {
  const { toast } = useToast();
  const { t } = useT();
  // A ref and not state: nothing renders from this, and re-rendering Layout on
  // every progress tick of every archive is exactly what the download watcher
  // beside it avoids by doing the same.
  const prev = useRef<Record<string, string>>({});
  useEffect(() => {
    return connectWS(
      (type, data) => {
        if (type !== 'extract') return;
        const job = data as ExtractJob;
        const before = prev.current[job.id];
        prev.current[job.id] = job.status;
        if (before === undefined || before === job.status) return;
        // The archive's own name, and the package as the fallback: a job whose
        // name is empty still has to say WHICH unpacking this was.
        const name = job.name || job.package || t('nav.downloads');
        if (job.status === 'done') toast(t('archive.doneToast', { name }), 'ok', 'extraction-done');
        else if (job.status === 'error') toast(t('archive.failedToast', { name }), 'fail', 'extraction-failed');
      },
      ['extract'],
    );
  }, [toast, t]);
}
