import { useState } from 'react';
import { deleteTasks, undoDelete, type Task } from '../lib/api';
import { adviceFor } from '../lib/failureAdvice';
import { message } from '../lib/intake';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconClose } from '../lib/icons';
import { useToast } from '../lib/toast';
import { Button, FieldGroup, Modal } from './ui';
import { CookieJarDialog } from './CookieJarDialog';

/**
 * FailureAdvice explains one failed download in a sentence, with at most one
 * remedy button, and shows the tool's raw error below it.
 *
 * It is a modal rather than a panel inside the row, because the virtual
 * scroller estimates unseen rows from the average height of drawn ones.
 */
export function FailureAdvice({
  task,
  base,
  reasonLabel,
  onClose,
}: {
  task: Task;
  base: string;
  /** The row's badge word for the cause, from columns.tsx's reasonKey. */
  reasonLabel: TranslationKey;
  onClose: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [cookies, setCookies] = useState(false);
  const [busy, setBusy] = useState(false);

  const advice = adviceFor(task.reason);

  // The cookie dialog replaces this one instead of stacking over it.
  if (cookies) return <CookieJarDialog task={task} base={base} onClose={onClose} />;
  if (!advice) return null;

  // No second confirmation: this removes the row, never the file, and the toast
  // offers an undo.
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

  // The cookie store is per instance and the peer proxy does not forward it, so
  // a peer's row gets the advice without the button.
  const canStoreCookies = base === '/api';
  const action =
    advice.action && (advice.fix !== 'cookies' || canStoreCookies) ? advice.action : undefined;

  return (
    <Modal
      title={t('failure.title')}
      onClose={onClose}
      footer={
        <>
          {/* Without a remedy, Close is the primary action. */}
          <Button
            kind={action ? 'secondary' : 'primary'}
            labelled
            icon={<IconClose />}
            title={t('failure.close')}
            disabled={busy}
            onClick={onClose}
          />
          {action && (
            <Button
              disabled={busy}
              onClick={() => (advice.fix === 'cookies' ? setCookies(true) : void remove())}
            >
              {t(action)}
            </Button>
          )}
        </>
      }
    >
      <FieldGroup label={t(reasonLabel)} hint={t(advice.hint)}>
        <div className="flex flex-col gap-1.5">
          <p className="text-sm text-carbon-text">{t(advice.line)}</p>
          {!action && <p className="text-xs text-carbon-textMuted">{t('failure.nothingToDo')}</p>}
        </div>
      </FieldGroup>

      {task.error && (
        <FieldGroup label={t('failure.raw')} hint={t('failure.rawHint')}>
          {/* Verbatim, since it gets pasted into searches and bug reports. */}
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
