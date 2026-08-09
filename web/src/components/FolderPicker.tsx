// The folder chooser: one dialog for every field in the app that holds a path.
//
// There are five of those - the download folder, the extraction destination,
// the watch folder, the add-links destination and the per-task override - and
// they all want the same three things: see what is on the server, walk into it,
// or type a path that does not exist yet. Built per page it would be built five
// times, and the rule below would be got wrong in at least one of them.
//
// THE RULE, and it is the whole reason this is not a plain file picker: a
// download folder may be a pathvars TEMPLATE, e.g.
// "/downloads/<jd:date>/<jd:hoster>". Only the part before the first placeholder
// is a real directory. Browsing may replace that part and NOTHING else - a
// chooser that wrote back the folder it landed on would silently delete the
// user's naming scheme, and they would not find out until six months of
// downloads had landed in one flat directory. The server does the splitting
// (GET /api/folders answers with `path` and `tail`), this file only has to put
// the two halves back together and never lose the second one.
import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '../lib/i18n';
import { IconArrowUp, IconFolder } from '../lib/icons';
import { Button, InfoBubble, Modal, TextInput } from './ui';
import { Tabs } from './Tabs';

/** One directory offered for the next click, as the server names it. */
interface FolderEntry {
  name: string;
  path: string;
}

/**
 * One place in the filesystem, as GET /api/folders describes it.
 *
 * `path` and `listed` differ when the folder is not there yet: `path` is what
 * was asked for, `listed` is the deepest folder above it that really exists and
 * the one `entries` describes. That is what lets the dialog say "this is new"
 * instead of showing an empty list and no explanation.
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
  // The server's own sentence, not a generic failure: "this instance may not
  // list /etc" and "there is no folder above /mnt/tank" are instructions, and a
  // dialog that swallows them leaves nothing on screen to act on.
  if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
  return (await r.json()) as Listing;
}

/** Trailing separators, so a path can be concatenated with a tail that has one. */
const TRAILING_SEP = /[\\/]+$/;

/**
 * joinTail puts a chosen folder back together with the template tail it arrived
 * with. This one function is the feature.
 *
 * The trailing separator comes off first because the tail carries its own
 * leading one - that is what makes a root ("/") plus "/<jd:date>" come out as
 * "/<jd:date>" and not "//<jd:date>". A path the user typed placeholders into
 * themselves is already a template and is taken as it stands: appending the tail
 * again would double the scheme they just wrote.
 */
export function joinTail(path: string, tail: string): string {
  if (!tail || path.includes('<')) return path;
  return path.replace(TRAILING_SEP, '') + tail;
}

const sameFolder = (a: string, b: string) => a.replace(TRAILING_SEP, '') === b.replace(TRAILING_SEP, '');

/**
 * under reports whether a path sits at or below a root.
 *
 * Not startsWith: "/mnt/archive" starts with "/mnt/a" and is not inside it, so a
 * plain prefix test would light up the wrong root the moment two of them share
 * an opening. The next character has to be a separator - either one, because the
 * server speaks whichever the host it runs on uses.
 */
function under(root: string, p: string): boolean {
  if (p === root) return true;
  const base = root.replace(TRAILING_SEP, '');
  if (!p.startsWith(base)) return false;
  const next = p.charAt(base.length);
  return next === '/' || next === '\\';
}

/**
 * PathInput is a folder field: the path itself, and the button that opens the
 * chooser beside it.
 *
 * The text box comes FIRST, and the order is load-bearing. These sit inside a
 * `Field`, which is a `<label>`, and a label hands a click on its caption to the
 * first labelable thing inside it - a `<button>` counts. With the button first,
 * clicking the word "Download folder" would open a dialog instead of putting the
 * cursor in the box.
 */
export function PathInput({
  value,
  onValue,
  placeholder,
  title,
}: {
  value: string;
  onValue: (next: string) => void;
  placeholder?: string;
  /** The chooser's heading, when the field is not the download folder. */
  title?: string;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);

  return (
    <span className="flex items-center gap-2">
      {/* dir="ltr" on a path is not cosmetic: in an RTL locale a path with a
          trailing slash renders with the slash on the wrong end, which is a path
          nobody can check by reading it. */}
      <TextInput
        dir="ltr"
        value={value}
        placeholder={placeholder}
        spellCheck={false}
        onChange={(e) => onValue(e.target.value)}
      />
      <Button
        type="button"
        kind="secondary"
        className="shrink-0"
        icon={<IconFolder width={16} height={16} />}
        title={t('folders.browse')}
        aria-label={t('folders.browse')}
        onClick={() => setOpen(true)}
      />
      {/* Into <body>, like the info bubble and for a sharper reason than
          clipping: this field lives inside a `Field`, which is a `<label>`, and
          a label forwards a click on anything non-interactive inside it to the
          control it names. Left here, every click on the dialog's backdrop or on
          its own background is also a click on the field behind it: focus jumps
          out of the dialog, and the backdrop closes it on the way. */}
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
  );
}

/**
 * FolderPicker is the dialog itself, for the callers that already have their own
 * field and only want the browsing.
 *
 * `value` is the field's current contents, template and all. `onPick` receives
 * the chosen folder with that template's tail already back on the end.
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
  // What the box says, and what has been listed. They are separate because the
  // box is the answer - "use this folder" takes whatever is typed in it, whether
  // or not anyone ever pressed Enter - while the listing is only what is on
  // screen to help.
  const [text, setText] = useState('');
  const [query, setQuery] = useState(value);
  const [data, setData] = useState<Listing | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  // The tail is taken from the FIRST answer and never again. Every later request
  // asks for a plain folder, which has no placeholders in it, so the server
  // rightly reports no tail - and overwriting it with that empty answer is
  // exactly how the user's naming scheme would get dropped on the first click.
  const [tail, setTail] = useState('');
  const seeded = useRef(false);

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
        // The last good listing stays on screen. A refusal that emptied the
        // dialog would leave no folder to click back to, and the way out of a
        // path you may not read is the list you came in through.
        if (live) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (live) setBusy(false);
      });
    return () => {
      live = false;
    };
  }, [query]);

  // Typing browses too, once it stops. The box is the fastest way to reach a
  // folder twelve levels down, and a path that lists nothing has to say so while
  // it is being typed rather than after the dialog is dismissed.
  useEffect(() => {
    if (!seeded.current) return;
    if (data && sameFolder(text, data.path)) return; // already looking at it
    const id = setTimeout(() => setQuery(text), 300);
    return () => clearTimeout(id);
  }, [text, data]);

  function navigate(path: string) {
    setText(path);
    setQuery(path);
  }

  // The deepest root the typed path sits under, so the strip marks where you
  // are. Longest match, because a narrowed boundary may nest one root inside
  // another and the closer one is the one that describes the position.
  const activeRoot =
    data?.roots.filter((r) => under(r, text)).sort((a, b) => b.length - a.length)[0] ?? null;

  const fresh = data && sameFolder(text, data.path) && !data.exists;

  return (
    <Modal
      title={title ?? t('folders.title')}
      onClose={onClose}
      footer={
        <>
          <Button onClick={() => onPick(joinTail(text, tail))} disabled={text.trim() === ''}>
            {t('folders.use')}
          </Button>
          <span className="flex-1" />
          <Button kind="ghost" onClick={onClose}>
            {t('common.cancel')}
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

      {/* Only when the boundary has more than one root. One tab saying "/" is a
          strip that chooses nothing. */}
      {data && data.roots.length > 1 && (
        <Tabs
          label={t('folders.roots')}
          size="sm"
          items={data.roots.map((r) => ({ id: r, label: r }))}
          active={activeRoot}
          // Arrow keys move without selecting here: every selection lists a
          // whole filesystem root, and walking the strip would fire one request
          // per keystroke.
          activateOnFocus={false}
          onSelect={navigate}
        />
      )}

      {/* A well, not a card. The dialog is already the one raised surface, and a
          card inside it would be the second.

          The height is bounded at both ends on purpose: a dialog that grows and
          shrinks with every folder you step into moves its own buttons out from
          under the pointer. */}
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

      {/* Three things the list cannot say for itself, in the order they matter:
          why the server refused, that the folder is about to be created, and
          that there were more folders than were sent. */}
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
