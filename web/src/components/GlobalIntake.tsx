import { useEffect } from 'react';
import { addLinks, uploadContainer } from '../lib/api';
import { isEditableTarget, message } from '../lib/intake';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';

/**
 * Makes the whole window - not one textarea - a target for a link, and for
 * the .txt/.dlc/.ccf/.rsdf files ContainerDrop's own small zone already
 * takes. Paste works the document over the same way, one Ctrl+V anywhere
 * that is not already a field of its own.
 *
 * Mounted once, beside CaptchaModal, for the same reason that one is: a
 * listener scoped to a single page's component tree only ever hears an
 * event that lands on that one page, and both the collector and the
 * download list are places a link or a container file lands (build-plan.md
 * section 8's Wave 8 note, 8B).
 *
 * Both listeners back off the moment isEditableTarget is true, and drop
 * additionally backs off once anything closer to the cursor has already
 * called preventDefault - TaskList's own column-header reorder and
 * Collector's own paste box both do, and checking e.defaultPrevented here
 * is the same pattern Collector's own context-menu handler already uses to
 * stay out of a more specific handler's way. Paste reads
 * event.clipboardData, never navigator.clipboard: the former needs no
 * permission and works on the bare http://192.168.x.x address this app
 * ordinarily runs behind, which is not a secure context and where
 * navigator.clipboard is undefined outright - see PasteFromClipboardButton
 * for the one control that explicitly does need it, and hides itself where
 * it cannot.
 */
export function GlobalIntake() {
  const { toast } = useToast();
  const { t } = useT();

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
      // Required for the drop below to ever fire with custom handling at
      // all - the browser's own default for an unhandled dragover is to
      // refuse the drop outright.
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

  return null;
}
