import { useEffect, useState } from 'react';
import { deleteTasks, fetchSiteLogins, restartTasks, undoDelete, type SiteLogin, type Task } from '../lib/api';
import { adviceFor } from '../lib/failureAdvice';
import { message } from '../lib/intake';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconClose, IconExternalLink, IconTrash, IconUndo } from '../lib/icons';
import { hostOf } from '../lib/searchQuery';
import { readsBrowsers, signInFor } from '../lib/siteLogins';
import { useToast } from '../lib/toast';
import { Button, FieldGroup, Modal } from './ui';
import { SiteLoginDialog } from './SiteLoginDialog';

/**
 * FailureAdvice explains one failed download in a sentence, with at most one
 * remedy button, and shows the tool's raw error below it.
 *
 * It is a modal rather than a panel inside the row, because the virtual
 * scroller estimates unseen rows from the average height of drawn ones.
 *
 * A bot check, a members-only answer or a video for signed-in accounts only,
 * from a site KnightLoader borrows a browser's sign-in for, is put right by
 * signing in there, and the download starts over once the session is there.
 * With a session already there, it most likely ran out.
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
  const [signingIn, setSigningIn] = useState(false);
  const [sites, setSites] = useState<SiteLogin[]>([]);
  const [busy, setBusy] = useState(false);

  const advice = adviceFor(task.reason);
  // The sign-ins are this instance's, and the peer proxy does not forward
  // them, so a peer's row gets the advice without the button.
  const local = base === '/api';
  const asksForSignIn = local && advice?.fix === 'signin';
  useEffect(() => {
    if (!asksForSignIn) return;
    void fetchSiteLogins().then(setSites, () => {});
  }, [asksForSignIn]);
  const signIn = signInFor(task.reason, hostOf(task), local ? sites : []);
  const again = signIn?.again && task.reason === 'membersOnly';

  // The sign-in window replaces this one instead of stacking over it.
  if (signingIn && signIn) {
    return (
      <SiteLoginDialog
        site={signIn.site}
        ways={{ readsBrowsers: readsBrowsers(sites) }}
        toSignIn
        onSignedIn={() => void restartTasks([task.id], base).catch(() => {})}
        onClose={onClose}
      />
    );
  }
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
              icon: <IconUndo />,
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

  const action = again ? 'failure.membersOnly.again.action' : advice.fix === 'signin' ? signIn && advice.action : advice.action;
  const words = signIn ? { site: signIn.site.name } : undefined;
  const line: TranslationKey = again ? 'failure.membersOnly.again.line' : advice.line;
  const hint: TranslationKey = again ? 'failure.membersOnly.again.hint' : signIn ? 'failure.signIn.hint' : advice.hint;

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
              icon={signIn ? <IconExternalLink /> : <IconTrash />}
              disabled={busy}
              onClick={() => (signIn ? setSigningIn(true) : void remove())}
            >
              {t(action, words)}
            </Button>
          )}
        </>
      }
    >
      <FieldGroup label={t(reasonLabel)} hint={t(hint, words)}>
        <div className="flex flex-col gap-1.5">
          <p className="text-sm text-carbon-text">{t(line, words)}</p>
          {!action && <p className="text-xs text-carbon-textMuted">{t('failure.nothingToDo')}</p>}
        </div>
      </FieldGroup>

      {task.error && (
        <FieldGroup label={t('failure.raw')} hint={t('failure.rawHint')}>
          {/* Verbatim, since it gets pasted into searches and bug reports. */}
          <div
            dir="ltr"
            className="glim-well max-h-40 overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-meta leading-relaxed text-carbon-textSub"
          >
            {task.error}
          </div>
        </FieldGroup>
      )}
    </Modal>
  );
}
