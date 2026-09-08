import { useState } from 'react';
import { deleteTasks, undoDelete, type Task } from '../lib/api';
import { adviceFor } from '../lib/failureAdvice';
import { message } from '../lib/intake';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, FieldGroup, Modal } from './ui';
import { CookieJarDialog } from './CookieJarDialog';

/**
 * One failed download, in one sentence and at most one button.
 *
 * IT IS AN OVERLAY, AND THAT IS NOT A STYLE CHOICE. The obvious shape for this
 * is a panel that unfolds inside the row, and it would quietly break the list:
 * the virtual scroller averages the height of the rows currently drawn and
 * sizes every row nobody has scrolled to from that average, so one 300px panel
 * among twenty 37px rows drags the estimate far enough that a long list
 * promises a scrollbar's worth of content that is not there. The same reasoning
 * is why the row's own failure line must stay one line and grow no prose.
 *
 * WHAT THE PERSON READS FIRST IS THIS APP'S READING, NOT THE TOOL'S. The tool's
 * own line is English, four hundred characters long and ends in links to a wiki
 * - it is evidence, not an explanation, so it goes below, labelled, in the
 * quiet well. The sentence above it is the one somebody can act on.
 *
 * AT MOST ONE BUTTON, AND ONLY WHERE IT HELPS. Two of the six causes end in a
 * sentence and stop; see lib/failureAdvice.ts for which and why. The third
 * restriction is here rather than in the table, because it is about WHERE the
 * row lives: the cookie store is this instance's alone (the peer proxy forwards
 * task routes and nothing else), so for a row belonging to another instance the
 * remedy is described and not offered. A button that stored a session on the
 * wrong machine would look like it worked.
 */
export function FailureAdvice({
  task,
  base,
  reasonLabel,
  onClose,
}: {
  task: Task;
  base: string;
  /**
   * The badge word the row already shows for this cause, handed in rather than
   * looked up here. columns.tsx's `reasonKey` stays the single table of those
   * words - a copy in this file is exactly how a dialog ends up naming a cause
   * something the row beside it never said.
   */
  reasonLabel: TranslationKey;
  onClose: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [cookies, setCookies] = useState(false);
  const [busy, setBusy] = useState(false);

  const advice = adviceFor(task.reason);

  // One window at a time: pressing the remedy REPLACES this dialog rather than
  // stacking a second one over it. The advice has already been read by then,
  // and two modals for one decision is two things to close.
  if (cookies) return <CookieJarDialog task={task} base={base} onClose={onClose} />;
  if (!advice) return null;

  // No second confirmation on top of a window that has just explained the
  // situation: this removes the row and never the bytes, and the undo is
  // offered on the message the way the toolbar's own removal offers it.
  const remove = async () => {
    setBusy(true);
    try {
      const r = await deleteTasks([task.id], false, base);
      const token = r.undo;
      toast(
        t('remove.done', { n: r.count }),
        'ok',
        'action-done',
        token
          ? {
              label: t('remove.undo'),
              run: async () => {
                try {
                  const back = await undoDelete(token, base);
                  if (back.count > 0) toast(t('remove.undone', { n: back.count }), 'ok');
                  else toast(t('remove.undoTooLate'), 'info');
                } catch (e) {
                  toast(t('list.failed', { error: message(e) }), 'fail');
                }
              },
              holdMs: r.undoMs,
            }
          : undefined,
      );
      onClose();
    } catch (e) {
      toast(t('list.failed', { error: message(e) }), 'fail');
    } finally {
      setBusy(false);
    }
  };

  // A jar reaches the download that needs it only when both live on the same
  // instance - see the doc comment above.
  const canStoreCookies = base === '/api';
  const action =
    advice.action && (advice.fix !== 'cookies' || canStoreCookies) ? advice.action : undefined;

  return (
    <Modal
      title={t('failure.title')}
      onClose={onClose}
      footer={
        <>
          {action && (
            <Button
              disabled={busy}
              onClick={() => (advice.fix === 'cookies' ? setCookies(true) : void remove())}
            >
              {t(action)}
            </Button>
          )}
          <Button kind={action ? 'secondary' : 'primary'} disabled={busy} onClick={onClose}>
            {t('failure.close')}
          </Button>
        </>
      }
    >
      <FieldGroup label={t(reasonLabel)} hint={t(advice.hint)}>
        <div className="flex flex-col gap-1.5">
          <p className="text-sm text-carbon-text">{t(advice.line)}</p>
          {/* Said out loud rather than left as an empty footer. "There is no
              button" is the answer to the question this window was opened
              with, and a window that simply has none reads as one that is
              still loading. */}
          {!action && <p className="text-xs text-carbon-textMuted">{t('failure.nothingToDo')}</p>}
        </div>
      </FieldGroup>

      {task.error && (
        <FieldGroup label={t('failure.raw')} hint={t('failure.rawHint')}>
          {/* dir="ltr" and left as it arrived: this is the tool's own output,
              and a line somebody is about to paste into a search or a bug
              report is worth nothing once this app has reflowed it. */}
          <div
            dir="ltr"
            className="glim-well max-h-40 overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-[11px] leading-relaxed text-carbon-textSub"
          >
            {task.error}
          </div>
        </FieldGroup>
      )}
    </Modal>
  );
}
