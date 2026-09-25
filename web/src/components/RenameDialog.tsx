// The windows that rename a link or a package, and what they say about a
// rename that cannot happen in full.
import { useEffect, useRef, useState } from 'react';
import { ApiError, type Task, refusal, renamePackage, setTaskOptions } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { IconClose } from '../lib/icons';
import { Button, Field, Modal, TextInput } from './ui';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** A refused rename's code (app.RenameRefusal) and what the window says for it. */
const REFUSALS: Partial<Record<string, TranslationKey>> = {
  torrent: 'rename.torrent',
  remote: 'rename.remote',
  unpacking: 'rename.unpacking',
  volume: 'rename.volume',
  empty: 'rename.empty',
  separator: 'rename.separator',
  dots: 'rename.dots',
  exists: 'rename.exists',
};

/**
 * renameRefusal says why the server refused a rename, in the reader's
 * language. Null means the refusal has no code this build knows, and the
 * caller shows the server's sentence.
 */
export function renameRefusal(e: unknown, t: Translate): string | null {
  if (!(e instanceof ApiError) || !e.code) return null;
  const key = REFUSALS[e.code];
  return key ? t(key, e.params) : null;
}

/**
 * linkRenameNote is what a link's rename window has to say beyond the name,
 * by the rules of app.renameLocked: that the name only lands once the download
 * has finished, or that it cannot land at all. Null means it applies at once.
 * A multi-volume part is only known once unpacking has numbered it; before
 * that the server's refusal says it.
 */
export function linkRenameNote(task: Task): { key: TranslationKey; blocked: boolean } | null {
  if (task.infoHash) return { key: 'rename.torrent', blocked: true };
  if (task.resolver === 'jd') return { key: 'rename.remote', blocked: true };
  if (task.status === 'extracting') return { key: 'rename.unpacking', blocked: true };
  if ((task.archivePart ?? 0) > 0) return { key: 'rename.volume', blocked: true };
  if (task.status === 'running') return { key: 'rename.whenDone', blocked: false };
  return null;
}

/**
 * hasFiles is app.hasFilesLocked as far as a row can tell. One such link in a
 * package is what makes a package rename keep the folder.
 */
export function hasFiles(task: Task): boolean {
  return task.status === 'running' || task.status === 'extracting' || task.status === 'done' || task.loaded > 0;
}

function RenameWindow({
  title,
  label,
  hint,
  current,
  note,
  blocked = false,
  stem = false,
  onRename,
  onClose,
}: {
  title: string;
  label: string;
  hint: string;
  current: string;
  /** A sentence about what the rename will not do, shown in the window. */
  note?: string;
  blocked?: boolean;
  /** Selects the name without its extension, as a file manager does. */
  stem?: boolean;
  onRename: (name: string) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [name, setName] = useState(current);
  const [busy, setBusy] = useState(false);
  const [shake, setShake] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const clean = name.trim();

  // Once, on opening: typing then replaces the name and keeps the extension.
  useEffect(() => {
    const dot = stem ? current.lastIndexOf('.') : -1;
    input.current?.setSelectionRange(0, dot > 0 ? dot : current.length);
  }, []);

  async function apply(): Promise<void> {
    if (blocked || busy || clean === '') return;
    if (clean === current) {
      onClose();
      return;
    }
    // The server refuses it too, but in English; this says it in the
    // reader's language before anything is sent.
    if (/[\\/]/.test(clean)) {
      toast(t('rename.separator'), 'fail');
      setShake((n) => n + 1);
      return;
    }
    setBusy(true);
    try {
      await onRename(clean);
    } catch (e) {
      setBusy(false);
      toast(renameRefusal(e, t) ?? t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
      setShake((n) => n + 1);
      return;
    }
    onClose();
  }

  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <Button shake={shake} disabled={blocked || busy || clean === ''} onClick={() => void apply()}>
            {t('rename.confirm')}
          </Button>
        </>
      }
    >
      <Field label={label} hint={hint}>
        <TextInput
          ref={input}
          autoFocus
          value={name}
          readOnly={blocked}
          spellCheck={false}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== 'Enter') return;
            e.preventDefault();
            void apply();
          }}
        />
      </Field>
      {note && <p className="text-sm text-carbon-textSub">{note}</p>}
    </Modal>
  );
}

/** RenameLinkDialog renames one link, which is the name its file is written under. */
export function RenameLinkDialog({ task, base, onClose }: { task: Task; base: string; onClose: () => void }) {
  const { t } = useT();
  const note = linkRenameNote(task);
  return (
    <RenameWindow
      title={t('rename.linkTitle')}
      label={t('props.name')}
      hint={t('rename.linkHint')}
      current={task.name}
      note={note ? t(note.key) : undefined}
      blocked={note?.blocked}
      // A link not resolved yet shows its URL, whose last dot is in the host.
      stem={task.name !== task.url}
      onClose={onClose}
      onRename={async (name) => {
        const r = await setTaskOptions([task.id], { name }, base);
        if (!r.ok) throw await refusal(r);
      }}
    />
  );
}

/**
 * RenamePackageDialog renames a package. `tasks` is every link of it in the
 * list the window was opened from, including rows a filter hides.
 */
export function RenamePackageDialog({
  name,
  tasks,
  base,
  onClose,
}: {
  name: string;
  tasks: Task[];
  base: string;
  onClose: () => void;
}) {
  const { t } = useT();
  return (
    <RenameWindow
      title={t('rename.packageTitle')}
      label={t('pkg.name')}
      hint={t('rename.packageHint')}
      current={name}
      note={tasks.some(hasFiles) ? t('rename.keepsFolder') : undefined}
      onClose={onClose}
      onRename={async (next) => {
        await renamePackage(
          tasks.map((x) => x.id),
          next,
          base,
        );
      }}
    />
  );
}
