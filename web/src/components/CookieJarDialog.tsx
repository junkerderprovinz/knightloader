import { useState } from 'react';
import {
  fetchSettings,
  patchSettings,
  restartTasks,
  saveYtdlpCookieJar,
  type Task,
} from '../lib/api';
import { message } from '../lib/intake';
import { hostOf } from '../lib/searchQuery';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, Field, Modal, TextArea, TextInput, ToggleRow } from './ui';

/**
 * The one window that stores a sign-in session for a site, opened from the
 * failure it fixes and from the Resolvers settings card alike.
 *
 * IT IS ONE PRESS OR IT IS NOTHING. A jar on its own does not download
 * anything: yt-dlp is only handed one while Settings.ytdlp.cookies is on, and
 * that switch is off on a fresh install. So a dialog that stored the paste and
 * stopped would fail the row again with the identical message - the worst
 * possible ending for a button whose whole promise is "this is the thing that
 * helps". Save therefore stores, arms and restarts, in that order.
 *
 * AND IT DOES NOT ARM ANYTHING BEHIND SOMEBODY'S BACK. The switch is
 * instance-wide - it decides whether every yt-dlp download in the app goes out
 * signed in, and a signed-in download has consequences a rate limit lands on
 * the account rather than on the address. So it is on screen, pre-armed, as
 * part of what Save is about to do, and never flipped silently from a window
 * that was opened about one link.
 *
 * THE JAR IS THIS MACHINE'S, THE RESTART IS THE TASK'S. /api/instances/{name}/
 * forwards the task, link and queue routes and nothing else (see
 * lib/controls.ts's own note), so the cookie store is always the local one
 * while the restart follows the task to whichever instance owns it. That is
 * also why FailureAdvice does not offer this window at all for a row on a peer:
 * storing a session here for a download that runs over there would look like it
 * worked and change nothing.
 */
export function CookieJarDialog({
  task,
  base = '/api',
  armSwitch = true,
  onClose,
  onSaved,
}: {
  /** The row this was opened from. Absent when the settings card opens it to
   *  add a site by hand: there is then no host to prefill and nothing to
   *  restart, and the window is a plain "store a jar". */
  task?: Task;
  /** Where the restart goes, never where the jar goes - see the doc comment. */
  base?: string;
  /**
   * False from the Resolvers settings card, whose own switch for the very same
   * setting sits two inches above this window. Two controls for one setting on
   * one screen is how a page ends up disagreeing with itself, and the card's
   * switch rides the shared Save bar while this one would write immediately.
   */
  armSwitch?: boolean;
  onClose: () => void;
  /** The list of sites with a stored jar, exactly as the server answers it
   *  after the write, so a caller that draws that list draws the answer and
   *  never the request. */
  onSaved?: (hosts: string[]) => void;
}) {
  const { t } = useT();
  const { toast } = useToast();

  // Prefilled and still editable. hostOf reads the task's own host, which is
  // the host of the LINK - for a site that serves its media from a separate
  // domain that is not the site somebody signed in to, and a jar stored under
  // it would be a jar nothing ever looks up.
  const [host, setHost] = useState(() => (task ? hostOf(task) : ''));
  const [text, setText] = useState('');
  // The form's intent, not a mirror of what is stored: after Save, cookies are
  // used. Left at true when the setting is already on, which is simply the
  // truth, and Save then patches nothing.
  const [arm, setArm] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const save = async () => {
    setBusy(true);
    setError('');
    try {
      // Stored first. If the host is refused, nothing else has happened: arming
      // an instance-wide feature and restarting a download on behalf of a paste
      // the server never accepted would be two consequences bought with a
      // failure.
      const hosts = await saveYtdlpCookieJar(host, text);
      if (armSwitch && arm) {
        // Read, spread, then patch. The settings document merges at its TOP
        // level only, so `{ ytdlp: { cookies: true } }` replaces the whole
        // yt-dlp block and empties the quality, the subtitle languages, the
        // output template and everything else the Resolvers page holds. Read
        // here rather than at mount, so an edit made in another tab while this
        // window stood open is not written back over.
        const s = await fetchSettings();
        if (!s.ytdlp.cookies) await patchSettings({ ytdlp: { ...s.ytdlp, cookies: true } });
      }
      if (task) {
        const r = await restartTasks([task.id], base);
        if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      }
      onSaved?.(hosts);
      toast(t('cookies.saved'), 'ok');
      onClose();
    } catch (e) {
      // The server's own sentence, which names the field and says what to send.
      // A string of ours here would be a vaguer second copy of it.
      setError(message(e).replace(/^(Error|ApiError):\s*/, ''));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title={task ? t('cookies.title', { host: hostOf(task) || task.url }) : t('cookies.addTitle')}
      onClose={onClose}
      footer={
        <>
          <Button
            // Both fields are required and neither has a useful empty meaning:
            // an empty jar is the CLEAR gesture on the server, and offering it
            // from a form whose host box is also empty would be a button that
            // deletes something nobody named.
            disabled={busy || host.trim() === '' || text.trim() === ''}
            onClick={() => void save()}
          >
            {task ? t('cookies.save') : t('settings.resolvers.cookieSave')}
          </Button>
          <Button kind="secondary" disabled={busy} onClick={onClose}>
            {t('common.cancel')}
          </Button>
          {error && <p className="min-w-0 text-xs text-statusWarn">{error}</p>}
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label={t('settings.resolvers.cookieHost')} hint={t('cookies.hostHint')}>
          <TextInput
            dir="ltr"
            spellCheck={false}
            value={host}
            placeholder={t('settings.resolvers.cookieHostPlaceholder')}
            disabled={busy}
            autoFocus={!task}
            onChange={(e) => setHost(e.target.value)}
          />
        </Field>
        <Field label={t('settings.resolvers.cookieText')} hint={t('cookies.jarHint')}>
          <TextArea
            rows={6}
            dir="ltr"
            spellCheck={false}
            value={text}
            placeholder={t('settings.resolvers.cookieTextPlaceholder')}
            disabled={busy}
            autoFocus={!!task}
            onChange={(e) => setText(e.target.value)}
          />
        </Field>
        {armSwitch && (
          <ToggleRow
            checked={arm}
            onChange={setArm}
            label={t('settings.resolvers.cookies')}
            hint={t('cookies.useStoredHint')}
            disabled={busy}
          />
        )}
      </div>
    </Modal>
  );
}
