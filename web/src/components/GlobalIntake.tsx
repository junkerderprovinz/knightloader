import { useEffect, useRef } from 'react';
import { addLinks, uploadContainer } from '../lib/api';
import { containerRefusal, isEditableTarget, message } from '../lib/intake';
import { useClipboardWatch, useClipboardWatchTarget } from '../lib/useClipboardWatch';
import { startClipboardWatch, type WatchOutcome } from '../lib/clipboardWatch';
import { startLease } from '../lib/clipboardWatchers';
import { isDesktop, onClipboardOutcome } from '../lib/desktop';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';

/**
 * GlobalIntake makes the whole window a paste and drop target for links and
 * container files. It stays out of editable fields and of drops a closer
 * handler already took.
 *
 * Paste reads event.clipboardData, which needs no permission and works on a
 * plain-http address where navigator.clipboard does not exist.
 */
export function GlobalIntake() {
  const { toast } = useToast();
  const { t } = useT();
  const [watch, setWatch] = useClipboardWatch();
  const [target] = useClipboardWatchTarget();

  useEffect(() => {
    async function stageText(text: string) {
      const links = text.trim();
      if (!links) return;
      try {
        const created = await addLinks(links, '');
        toast(
          created.length ? t('collector.toastStaged', { n: created.length }) : t('collector.toastNone'),
          created.length ? 'ok' : 'fail',
        );
      } catch (e) {
        toast(t('list.failed', { error: message(e) }), 'fail');
      }
    }

    async function stageFile(file: File) {
      try {
        const r = await uploadContainer(file);
        if (r.handedTo === 'jd') {
          toast(t('container.handed', { file: file.name, n: r.expiresIn }), 'info');
        } else if (r.handedTo === 'usenet') {
          toast(t('container.usenet', { file: file.name, service: r.service }), 'info');
        } else if (r.created.length > 0) {
          toast(t('container.staged', { n: r.created.length, file: file.name }), 'ok');
        } else {
          toast(t('container.allKnown', { file: file.name, n: r.links }), 'info');
        }
      } catch (e) {
        toast(t('container.failed', { file: file.name, reason: containerRefusal(t, e) }), 'fail');
      }
    }

    function onPaste(e: ClipboardEvent) {
      if (isEditableTarget(e.target)) return;
      const text = e.clipboardData?.getData('text/plain') ?? '';
      if (!text.trim()) return;
      e.preventDefault();
      void stageText(text);
    }

    function onDragOver(e: DragEvent) {
      if (isEditableTarget(e.target)) return;
      // Without this the browser refuses the drop.
      e.preventDefault();
    }

    function onDrop(e: DragEvent) {
      if (e.defaultPrevented || isEditableTarget(e.target)) return;
      const files = [...(e.dataTransfer?.files ?? [])];
      if (files.length > 0) {
        e.preventDefault();
        for (const file of files) void stageFile(file);
        return;
      }
      const text = e.dataTransfer?.getData('text/plain') || e.dataTransfer?.getData('text/uri-list') || '';
      if (!text.trim()) return;
      e.preventDefault();
      void stageText(text);
    }

    document.addEventListener('paste', onPaste);
    window.addEventListener('dragover', onDragOver);
    window.addEventListener('drop', onDrop);
    return () => {
      document.removeEventListener('paste', onPaste);
      window.removeEventListener('dragover', onDragOver);
      window.removeEventListener('drop', onDrop);
    };
  }, [t, toast]);

  // A ref, so the language arriving after the first render does not restart
  // the watch, which would drop its lease and take it again.
  const latest = useRef({ t, toast });
  latest.current = { t, toast };

  // The clipboard watch lives here because this component stays mounted across
  // pages. A refused permission ends the watch instead of asking again. In the
  // desktop app the watch runs in Go, which also holds its lease, and the page
  // only shows what it did, so a link is not sent twice while the window has
  // focus. In a browser the lease keeps this tab on the group's list of
  // watchers, held with the instance the links go to. Either way, another
  // device switching the watch off there ends it here.
  useEffect(() => {
    if (!watch) return;
    const stoppedElsewhere = () => {
      const { t, toast } = latest.current;
      setWatch(false);
      toast(t('intake.clipboardWatchStoppedElsewhere'), 'info');
    };
    const show = (o: WatchOutcome) => {
      const { t, toast } = latest.current;
      switch (o.kind) {
        case 'staged':
          toast(t('collector.toastStaged', { n: o.n }), 'ok');
          break;
        case 'none':
          // Usually a re-copied link already in the collector.
          break;
        case 'denied':
          setWatch(false);
          toast(t('intake.clipboardWatchDenied', { reason: o.reason }), 'fail');
          break;
        case 'failed':
          toast(t('list.failed', { error: o.reason }), 'fail');
          break;
        case 'limited':
          toast(t('intake.clipboardWatchLimited'), 'info');
          break;
        case 'stopped':
          stoppedElsewhere();
          break;
      }
    };
    if (isDesktop()) return onClipboardOutcome(show);
    const endLease = startLease(target, stoppedElsewhere);
    const endWatch = startClipboardWatch(target, show);
    return () => {
      endWatch();
      endLease();
    };
  }, [watch, setWatch, target]);

  return null;
}
