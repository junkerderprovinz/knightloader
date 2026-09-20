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
 * CookieJarDialog stores a sign-in cookie jar for a site, opened from a failed
 * row or from the Resolvers settings card.
 *
 * A jar alone changes nothing while the instance-wide yt-dlp cookie switch is
 * off, so Save stores, arms and restarts, with the switch shown pre-armed
 * rather than flipped silently. The jar is always stored locally; only the
 * restart follows `base`, because the peer proxy forwards task routes only.
 */
export function CookieJarDialog({
  task,
  base = '/api',
  armSwitch = true,
  onClose,
  onSaved,
}: {
  /** The row this was opened from; absent when adding a site by hand. */
  task?: Task;
  /** Where the restart goes, never the jar. */
  base?: string;
  /** False from the settings card, which has its own switch for the setting. */
  armSwitch?: boolean;
  onClose: () => void;
  /** Receives the server's list of hosts with a stored jar after the write. */
  onSaved?: (hosts: string[]) => void;
}) {
  const { t } = useT();
  const { toast } = useToast();

  // Editable, because a site serving media from another domain needs the
  // sign-in domain rather than the link's host.
  const [host, setHost] = useState(() => (task ? hostOf(task) : ''));
  const [text, setText] = useState('');
  // The form's intent; Save patches nothing if the setting is already on.
  const [arm, setArm] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const save = async () => {
    setBusy(true);
    setError('');
    try {
      // Stored first, so a refused host arms and restarts nothing.
      const hosts = await saveYtdlpCookieJar(host, text);
      if (armSwitch && arm) {
        // Settings merge at the top level only, so the whole ytdlp block is
        // sent back. Read now rather than at mount to keep edits from another tab.
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
      // The server's sentence names the field and what to send.
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
          {/* The forward button ends the row, so the message goes first. */}
          {error && <p className="min-w-0 text-xs text-statusWarn">{error}</p>}
          <span className="flex-1" />
          <Button kind="secondary" disabled={busy} onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            // An empty jar means "clear" to the server, so both fields are required.
            disabled={busy || host.trim() === '' || text.trim() === ''}
            onClick={() => void save()}
          >
            {task ? t('cookies.save') : t('settings.resolvers.cookieSave')}
          </Button>
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
