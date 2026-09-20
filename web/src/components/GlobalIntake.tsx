import { useEffect } from 'react';
import { addLinks, uploadContainer } from '../lib/api';
import { isEditableTarget, message } from '../lib/intake';
import { useClipboardWatch } from '../lib/useClipboardWatch';
import { startClipboardWatch } from '../lib/clipboardWatch';
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
        } else if (r.created.length > 0) {
          toast(t('container.staged', { n: r.created.length, file: file.name }), 'ok');
        } else {
          toast(t('container.allKnown', { file: file.name, n: r.links }), 'info');
        }
      } catch (e) {
        toast(t('container.failed', { file: file.name, reason: message(e) }), 'fail');
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

  // The clipboard watch lives here because this component stays mounted across
  // pages. A refused permission ends the watch instead of asking again.
  useEffect(() => {
    if (!watch) return;
    return startClipboardWatch((o) => {
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
      }
    });
  }, [watch, setWatch, t, toast]);

  return null;
}
