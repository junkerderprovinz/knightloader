// The folder chooser shared by every path field. A download folder may be a
// template such as "/downloads/<jd:date>/<jd:hoster>", and browsing may only
// replace the real directory before the first placeholder. The server splits
// the path into `path` and `tail`; this file puts them back together without
// ever dropping the tail.
import { useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ApiError } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconArrowUp, IconCheck, IconClose, IconFolder, IconFolderPlus } from '../lib/icons';
import { Button, InfoBubble, Modal, TextInput } from './ui';
import { Tabs } from './Tabs';

interface FolderEntry {
  name: string;
  path: string;
}

/**
 * Listing is GET /api/folders' answer. For a folder that does not exist yet,
 * `listed` is the deepest existing parent, the one `entries` describes.
 */
interface Listing {
  path: string;
  tail: string;
  exists: boolean;
  listed: string;
  parent: string;
  roots: string[];
  entries: FolderEntry[];
  truncated: boolean;
}

async function fetchFolders(path: string): Promise<Listing> {
  const r = await fetch(`/api/folders?path=${encodeURIComponent(path)}`);
  // The server's sentence says what is refused and why.
  if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
  return (await r.json()) as Listing;
}

/**
 * makeFolder creates an empty folder inside `parent` on the server and returns
 * its path. A refusal is an ApiError whose code names the reason.
 */
async function makeFolder(parent: string, name: string): Promise<string> {
  const r = await fetch('/api/folders', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ parent, name }),
  });
  // A proxy in front of the server can answer with a page of its own.
  const body = (await r.json().catch(() => ({}))) as { path?: string; error?: string; code?: string };
  if (!r.ok || !body.path) throw new ApiError(body.error || String(r.status), body.code, undefined, r.status);
  return body.path;
}

/** The refusals a person can act on. The rest show the server's sentence. */
const REFUSALS: Partial<Record<string, TranslationKey>> = {
  exists: 'folders.error.exists',
  denied: 'folders.error.denied',
  outside: 'folders.error.outside',
  missing: 'folders.error.missing',
  separator: 'folders.error.separator',
  dots: 'folders.error.dots',
  character: 'folders.error.character',
  trailing: 'folders.error.trailing',
  reserved: 'folders.error.reserved',
  tooLong: 'folders.error.tooLong',
};

const TRAILING_SEP = /[\\/]+$/;

/**
 * joinTail appends the template tail to a chosen folder. The tail brings its
 * own leading separator, and a path that already contains placeholders is
 * taken as it stands.
 */
export function joinTail(path: string, tail: string): string {
  if (!tail || path.includes('<')) return path;
  return path.replace(TRAILING_SEP, '') + tail;
}

const sameFolder = (a: string, b: string) => a.replace(TRAILING_SEP, '') === b.replace(TRAILING_SEP, '');

/**
 * under reports whether a path sits at or below a root. A prefix match alone
 * would put "/mnt/archive" under "/mnt/a", so the next character must be a
 * separator of either kind.
 */
function under(root: string, p: string): boolean {
  if (p === root) return true;
  const base = root.replace(TRAILING_SEP, '');
  if (!p.startsWith(base)) return false;
  const next = p.charAt(base.length);
  return next === '/' || next === '\\';
}

/**
 * PathInput is a path field with a browse button. The text box comes first
 * because the surrounding Field is a <label>, which forwards a caption click to
 * its first labelable child.
 */
export function PathInput({
  value,
  onValue,
  placeholder,
  title,
  label,
  autoFocus,
  local = true,
  error,
}: {
  value: string;
  onValue: (next: string) => void;
  placeholder?: string;
  /** The chooser's heading, when the field is not the download folder. */
  title?: string;
  /** The text box's name, for a field that stands under a row title rather than inside a Field. */
  label?: string;
  autoFocus?: boolean;
  /**
   * False for a folder on a peer instance. The peer proxy does not forward
   * the chooser, which would browse this machine's disk instead, so the field
   * has no browse button there.
   */
  local?: boolean;
  /** Why the server refused what the box holds, shown beneath it. */
  error?: string;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const errorId = useId();

  return (
    <span className="flex flex-col gap-1">
      <span className="flex items-center gap-2">
        {/* In an RTL locale a trailing slash would render on the wrong end. */}
        <TextInput
          aria-label={label}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          autoFocus={autoFocus}
          // Marked, not corrected: the text stays as typed.
          className={error ? 'shadow-[0_0_0_2px_var(--status-warn-text)]' : ''}
          dir="ltr"
          value={value}
          placeholder={placeholder}
          spellCheck={false}
          onChange={(e) => onValue(e.target.value)}
        />
        {local && (
          <Button
            type="button"
            kind="secondary"
            className="shrink-0"
            icon={<IconFolder width={16} height={16} />}
            title={t('folders.browse')}
            aria-label={t('folders.browse')}
            onClick={() => setOpen(true)}
          />
        )}
        {/* Portalled out of the <label>, which would forward every click inside
            the dialog to the field behind it. */}
        {open &&
          createPortal(
            <FolderPicker
              value={value}
              title={title}
              onClose={() => setOpen(false)}
              onPick={(next) => {
                onValue(next);
                setOpen(false);
              }}
            />,
            document.body,
          )}
      </span>
      {error && (
        <span id={errorId} className="text-xs text-statusWarn">
          {error}
        </span>
      )}
    </span>
  );
}

/**
 * FolderPicker is the browsing dialog on its own. `value` is the field's
 * contents, template included, and `onPick` receives the chosen folder with
 * the template tail put back.
 */
export function FolderPicker({
  value,
  onPick,
  onClose,
  title,
}: {
  value: string;
  onPick: (next: string) => void;
  onClose: () => void;
  title?: string;
}) {
  const { t } = useT();
  // The box is the answer, typed or browsed; the listing only helps.
  const [text, setText] = useState('');
  const [query, setQuery] = useState(value);
  const [data, setData] = useState<Listing | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  // Taken from the first answer only: later requests name plain folders, for
  // which the server reports an empty tail.
  const [tail, setTail] = useState('');
  const seeded = useRef(false);
  // The row that names a new folder, in place of its button while open.
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState('');
  const [nameError, setNameError] = useState('');
  const [making, setMaking] = useState(false);
  const newId = useId();
  const errorId = useId();

  useEffect(() => {
    let live = true;
    setBusy(true);
    fetchFolders(query)
      .then((d) => {
        if (!live) return;
        setData(d);
        setError('');
        if (!seeded.current) {
          seeded.current = true;
          setText(d.path);
          setTail(d.tail);
        }
      })
      .catch((e: unknown) => {
        // The last good listing stays, so there is a way back.
        if (live) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (live) setBusy(false);
      });
    return () => {
      live = false;
    };
  }, [query]);

  // Typing browses too, once it pauses.
  useEffect(() => {
    if (!seeded.current) return;
    if (data && sameFolder(text, data.path)) return;
    const id = setTimeout(() => setQuery(text), 300);
    return () => clearTimeout(id);
  }, [text, data]);

  function navigate(path: string) {
    setText(path);
    setQuery(path);
  }

  // The row takes the button's place, so focus has to be handed back to it
  // once the row is gone.
  function closeNaming() {
    setNaming(false);
    setName('');
    setNameError('');
    requestAnimationFrame(() => document.getElementById(newId)?.focus());
  }

  async function create() {
    const clean = name.trim();
    if (!data || clean === '' || making) return;
    setMaking(true);
    try {
      const made = await makeFolder(data.listed, clean);
      closeNaming();
      navigate(made);
    } catch (e) {
      const key = e instanceof ApiError && e.code ? REFUSALS[e.code] : undefined;
      setNameError(key ? t(key, { name: clean }) : e instanceof Error ? e.message : String(e));
    } finally {
      setMaking(false);
    }
  }

  // The longest matching root, since roots can nest.
  const activeRoot =
    data?.roots.filter((r) => under(r, text)).sort((a, b) => b.length - a.length)[0] ?? null;

  const fresh = data && sameFolder(text, data.path) && !data.exists;

  return (
    <Modal
      title={title ?? t('folders.title')}
      onClose={onClose}
      footer={
        <>
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <span className="flex-1" />
          <Button onClick={() => onPick(joinTail(text, tail))} disabled={text.trim() === ''}>
            {t('folders.use')}
          </Button>
        </>
      }
    >
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(text);
        }}
      >
        <TextInput
          dir="ltr"
          autoFocus
          value={text}
          spellCheck={false}
          aria-label={t('folders.path')}
          onChange={(e) => setText(e.target.value)}
        />
        <Button
          type="button"
          kind="secondary"
          className="shrink-0"
          icon={<IconArrowUp width={16} height={16} />}
          title={t('folders.up')}
          aria-label={t('folders.up')}
          disabled={!data?.parent}
          onClick={() => data?.parent && navigate(data.parent)}
        />
      </form>

      {tail && (
        <p className="flex min-w-0 items-center gap-2 text-xs">
          <span className="glim-eyebrow shrink-0">{t('folders.tail')}</span>
          <code dir="ltr" className="truncate text-xs text-carbon-text">
            {tail}
          </code>
          <InfoBubble tip={t('folders.tailHint')} />
        </p>
      )}

      {data && data.roots.length > 1 && (
        <Tabs
          label={t('folders.roots')}
          size="sm"
          items={data.roots.map((r) => ({ id: r, label: r }))}
          active={activeRoot}
          // Each selection lists a whole root, so arrow keys only move focus.
          activateOnFocus={false}
          onSelect={navigate}
        />
      )}

      {/* Bounded height, so the buttons stay put while browsing. */}
      <div dir="ltr" className="glim-well max-h-64 min-h-32 overflow-y-auto py-1">
        {!data && busy && (
          <p className="px-3 py-6 text-center text-xs text-carbon-textMuted">{t('common.loading')}</p>
        )}
        {data?.entries.map((e) => (
          <button
            key={e.path}
            type="button"
            onClick={() => navigate(e.path)}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-start text-sm
              text-carbon-text transition-colors hover:bg-carbon-hover"
          >
            <IconFolder width={16} height={16} className="shrink-0 text-carbon-textMuted" />
            <span className="truncate">{e.name}</span>
          </button>
        ))}
        {data && data.entries.length === 0 && (
          <p className="px-3 py-6 text-center text-xs text-carbon-textMuted">{t('folders.empty')}</p>
        )}
      </div>

      {naming ? (
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <TextInput
            autoFocus
            dir="auto"
            value={name}
            spellCheck={false}
            aria-label={t('folders.newName')}
            placeholder={t('folders.newName')}
            aria-invalid={nameError !== ''}
            aria-describedby={nameError ? errorId : undefined}
            onChange={(e) => {
              setName(e.target.value);
              setNameError('');
            }}
            onKeyDown={(e) => {
              // Escape gives up the name, not the whole chooser.
              if (e.key === 'Escape') {
                e.stopPropagation();
                closeNaming();
              }
            }}
          />
          <Button
            type="button"
            kind="ghost"
            className="shrink-0"
            icon={<IconClose width={16} height={16} />}
            title={t('common.cancel')}
            onClick={closeNaming}
          />
          {/* Secondary, since "Use this folder" is the window's one accent button. */}
          <Button
            type="submit"
            kind="secondary"
            className="shrink-0"
            icon={<IconCheck width={16} height={16} />}
            disabled={name.trim() === '' || making}
          >
            {t('folders.create')}
          </Button>
        </form>
      ) : (
        <div>
          <Button
            type="button"
            id={newId}
            kind="secondary"
            icon={<IconFolderPlus width={16} height={16} />}
            hint={t('folders.newFolderHint')}
            disabled={!data}
            onClick={() => setNaming(true)}
          >
            {t('folders.newFolder')}
          </Button>
        </div>
      )}
      {nameError && (
        // Auto, because the server's own sentence is English in every locale.
        <p id={errorId} role="alert" dir="auto" className="text-xs text-statusFail">
          {nameError}
        </p>
      )}

      {error && <p className="text-xs text-statusFail">{error}</p>}
      {fresh && <p className="text-xs text-statusWarn">{t('folders.new')}</p>}
      {data?.truncated && (
        <p className="glim-num text-xs text-carbon-textMuted">
          {t('folders.truncated', { n: data.entries.length })}
        </p>
      )}
    </Modal>
  );
}
