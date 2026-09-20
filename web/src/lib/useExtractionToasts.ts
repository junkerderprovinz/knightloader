// The global unpacking watcher, the source of the extraction rows in the
// notification matrix (lib/notify.ts). Mounted once in Layout beside
// useCompletionToasts, so it watches for the whole session.
//
// It notifies on a change of status only: the server broadcasts a job
// repeatedly while it runs, and a job first seen already finished stays
// quiet. 'cancelled' never notifies, since it is the user's own abort.
import { useEffect, useRef } from 'react';
import { connectWS, type ExtractJob } from './api';
import { useT } from './i18n';
import { useToast } from './toast';

export function useExtractionToasts() {
  const { toast } = useToast();
  const { t } = useT();
  // A ref, so progress updates do not re-render Layout.
  const prev = useRef<Record<string, string>>({});
  useEffect(() => {
    return connectWS(
      (type, data) => {
        if (type !== 'extract') return;
        const job = data as ExtractJob;
        const before = prev.current[job.id];
        prev.current[job.id] = job.status;
        if (before === undefined || before === job.status) return;
        // The package stands in for a job without a name.
        const name = job.name || job.package || t('nav.downloads');
        if (job.status === 'done') toast(t('archive.doneToast', { name }), 'ok', 'extraction-done');
        else if (job.status === 'error') toast(t('archive.failedToast', { name }), 'fail', 'extraction-failed');
      },
      ['extract'],
    );
  }, [toast, t]);
}
