// The collector's file intake for everything that isn't a plain pasted link
// (AddLinksForm already owns that): a .torrent file, or a link-container file
// (.txt/.dlc/.ccf/.rsdf). Reached two ways - AddLinksForm's own folder-icon
// badge opens the picker below through this component's ref, and (jdp,
// 2026-08-24) the same paste box's own drop target hands dropped FILES here
// too, so this component keeps no drop target of its own - see
// FileDropHandle's own doc comment.
//
// Replaces the two former one-row bars (ContainerDrop, TorrentUpload) - same
// underlying requests (parseTorrentUpload/stageTorrent, uploadContainer), one
// intake path instead of two, so a person does not have to guess which of two
// near-identical surfaces a given file belongs on. A file is tried as a
// torrent first (server sniffs the bencoded bytes, not the extension) and
// falls back to the container endpoint on failure. What the file picker's
// `accept` offers is the union of both formats - a convenience for the
// picker, never a gate, matching ContainerDrop/TorrentUpload's own
// established reasoning: the server decides by content, so a misnamed file
// chosen anyway is sent as it is.
import { forwardRef, useImperativeHandle, useRef, useState } from 'react';
import { parseTorrentUpload, stageTorrent, uploadContainer, type Task, type TorrentTree } from '../lib/api';
import { fmtBytes } from '../lib/format';
import { message } from '../lib/intake';
import { useT } from '../lib/i18n';
import { Button } from './ui';
import { IconCheck } from '../lib/icons';

/** What Collector.tsx reaches through the ref for: opening the file picker
 *  from AddLinksForm's own button row (jdp: "Dropzone mit Dateiwählen
 *  button neben dem Zum-Sammler-Button") instead of FileDrop's own, since
 *  the two share one destination and one moment somebody is done choosing
 *  what to add. handleFiles is the same reason, extended to drag-and-drop
 *  (jdp, 2026-08-24: "können wir diesen text und card nicht entfernen" —
 *  AddLinksForm's own paste box now accepts a file drop directly and hands
 *  the files here instead of this component keeping a second, visible drop
 *  target of its own; see this file's own top-of-file comment for why the
 *  visible row is gone but the handling underneath it is not). */
export interface FileDropHandle {
  openPicker: () => void;
  handleFiles: (files: File[]) => void;
}

const FILE_ACCEPT = '.torrent,.txt,.dlc,.ccf,.rsdf';

// Mirrors container.MaxBytes in internal/container - only the container path
// has a size ceiling; a torrent's own bencoded metadata is small by nature.
const MAX_CONTAINER_BYTES = 8 << 20;

/** One file's answer, kept structured so the sentence is built at render time
 *  and follows a language change instead of freezing at upload time. */
type Outcome =
  | { file: string; kind: 'container-staged'; links: number; created: number; pkg: string }
  | { file: string; kind: 'container-handed'; expiresIn: number }
  | { file: string; kind: 'torrent-staged'; task: Task }
  | { file: string; kind: 'torrent-duplicate' }
  | { file: string; kind: 'failed'; reason: string };

function Result({ o }: { o: Outcome }) {
  const { t } = useT();

  if (o.kind === 'failed') {
    return <p className="px-4 text-xs text-statusFail">{t('container.failed', { file: o.file, reason: o.reason })}</p>;
  }
  if (o.kind === 'torrent-duplicate') {
    return <p className="px-4 text-xs text-carbon-textSub">{t('torrent.duplicate', { file: o.file })}</p>;
  }
  if (o.kind === 'torrent-staged') {
    return (
      <p className="px-4 text-xs text-statusOk">
        {o.task.package ? t('torrent.stagedIn', { file: o.file, pkg: o.task.package }) : t('torrent.staged', { file: o.file })}
      </p>
    );
  }
  // Nothing is staged yet in this case, and saying "0 links added" is what makes
  // people upload the same file four times. The links arrive over the websocket
  // when the backend has fetched the handover.
  if (o.kind === 'container-handed') {
    return <p className="px-4 text-xs text-carbon-textSub">{t('container.handed', { file: o.file, n: o.expiresIn })}</p>;
  }
  // The container held links and none of them became a task: every one was
  // already in the list. Not a fault, and not silence either.
  if (o.created === 0) {
    return <p className="px-4 text-xs text-carbon-textSub">{t('container.allKnown', { file: o.file, n: o.links })}</p>;
  }
  const known = o.links - o.created;
  return (
    <p className="px-4 text-xs text-statusOk">
      {o.pkg ? t('container.stagedIn', { n: o.created, file: o.file, pkg: o.pkg }) : t('container.staged', { n: o.created, file: o.file })}
      {known > 0 && ` ${t('container.alsoKnown', { n: known })}`}
    </p>
  );
}

// Pending is the torrent file-tree review step: a parsed .torrent with more
// than one file inside it, waiting for a person to check/uncheck files
// before staging continues - see docs/torrent-support.md's UI section. A
// container upload has no equivalent review step (it stages every link it
// finds outright), so this is torrent-only. selected is indexed the same as
// tree.files.
interface Pending {
  file: string;
  tree: TorrentTree;
  selected: boolean[];
}

/** TorrentFileRow is one line of the tree: the whole row is the control, not
 *  a checkbox nested inside one - same reasoning CollectorFacets.tsx's own
 *  FacetRow already applies right next to this page. */
function TorrentFileRow({
  path,
  size,
  checked,
  onToggle,
}: {
  path: string;
  size: number;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      onClick={onToggle}
      className="flex w-full items-center gap-2 px-2.5 py-1.5 text-start text-xs text-carbon-textSub transition-colors hover:bg-carbon-hover"
    >
      <span
        aria-hidden
        className={`grid h-4.5 w-4.5 shrink-0 place-items-center rounded-[var(--radius-control)] transition-colors ${
          checked ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3/60 text-transparent'
        }`}
      >
        <IconCheck width={12} height={12} />
      </span>
      {/* Paths read left-to-right even in a right-to-left interface. */}
      <span dir="ltr" className="min-w-0 flex-1 truncate text-start" title={path}>
        {path}
      </span>
      <span className="glim-num shrink-0 text-carbon-textMuted">{fmtBytes(size)}</span>
    </button>
  );
}

function TorrentTreeCard({
  pending,
  onChange,
  onCancel,
  onConfirm,
  busy,
}: {
  pending: Pending;
  onChange: (selected: boolean[]) => void;
  onCancel: () => void;
  onConfirm: () => void;
  busy: boolean;
}) {
  const { t } = useT();
  const selectedCount = pending.selected.filter(Boolean).length;
  const selectedSize = pending.tree.files.reduce((sum, f, i) => (pending.selected[i] ? sum + f.size : sum), 0);

  function setAll(v: boolean) {
    onChange(pending.tree.files.map(() => v));
  }
  function toggle(i: number) {
    const next = [...pending.selected];
    next[i] = !next[i];
    onChange(next);
  }

  return (
    <div className="glim-card flex flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-carbon-text" title={pending.tree.name}>
            {pending.tree.name}
          </p>
          <p className="text-xs text-carbon-textSub">
            {t('torrent.tree.summary', { n: selectedCount, total: pending.tree.files.length, size: fmtBytes(selectedSize) })}
          </p>
        </div>
        {pending.tree.private && (
          <span className="shrink-0 rounded-[var(--radius-pill)] bg-carbon-surface3 px-2 py-0.5 text-[11px] text-carbon-textSub">
            {t('torrent.tree.private')}
          </span>
        )}
      </div>

      <div className="flex items-center gap-3 text-xs">
        <button type="button" className="text-accent hover:underline" onClick={() => setAll(true)}>
          {t('torrent.tree.selectAll')}
        </button>
        <button type="button" className="text-accent hover:underline" onClick={() => setAll(false)}>
          {t('torrent.tree.selectNone')}
        </button>
      </div>

      <div className="max-h-64 overflow-y-auto rounded-[var(--radius-control)] bg-carbon-surface2">
        {pending.tree.files.map((f, i) => (
          <TorrentFileRow key={f.path} path={f.path} size={f.size} checked={pending.selected[i]} onToggle={() => toggle(i)} />
        ))}
      </div>

      <div className="flex justify-end gap-2">
        <Button kind="ghost" onClick={onCancel} disabled={busy}>
          {t('torrent.tree.cancel')}
        </Button>
        <Button kind="primary" onClick={onConfirm} disabled={busy || selectedCount === 0}>
          {busy ? t('torrent.staging') : t('torrent.tree.add')}
        </Button>
      </div>
    </div>
  );
}

/**
 * FileDrop is the collector's file intake: what used to be ContainerDrop and
 * TorrentUpload, then a merged visible drop row of its own, now a purely
 * reactive component with no visible surface of its own most of the time
 * (jdp, 2026-08-24: "können wir diesen text und card nicht entfernen" — the
 * hint text and its row are gone; AddLinksForm's own paste box is the one
 * drop target now, for both text and files, and reaches the handling below
 * through the ref rather than this component keeping a second target
 * beside it). It still renders something the moment there is something to
 * show: the torrent file-tree review card, or a batch's outcome lines.
 */
export const FileDrop = forwardRef<FileDropHandle, { pkg?: string }>(function FileDrop({ pkg = '' }, ref) {
  const { t } = useT();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<Outcome[]>([]);
  const [pending, setPending] = useState<Pending | null>(null);

  useImperativeHandle(ref, () => ({
    // Guarded here, not by the caller: sendFiles refuses the same way while
    // busy or under tree review, so a picker opened past that point would
    // stage a second batch that only shows up once the button is pressed
    // again - the guard belongs next to the state it protects.
    openPicker: () => {
      if (!busy && !pending) input.current?.click();
    },
    handleFiles: (files: File[]) => void sendFiles(files),
  }));

  async function commitTorrent(file: string, tree: TorrentTree, selected: boolean[]): Promise<Outcome> {
    try {
      // undefined (every file kept) rather than the full path list when
      // nothing was unticked, matching stageTorrent's own documented default.
      const selectedPaths = selected.every(Boolean) ? undefined : tree.files.filter((_, i) => selected[i]).map((f) => f.path);
      const task = await stageTorrent(tree.uri, pkg, selectedPaths);
      return task ? { file, kind: 'torrent-staged', task } : { file, kind: 'torrent-duplicate' };
    } catch (e) {
      return { file, kind: 'failed', reason: message(e) };
    }
  }

  async function confirmPending() {
    if (!pending) return;
    setBusy(true);
    const out = await commitTorrent(pending.file, pending.tree, pending.selected);
    setResults((r) => [...r, out]);
    setBusy(false);
    setPending(null);
  }

  /** Tries one file as a torrent first, falls back to the container endpoint.
   *  Returns the finished Outcome, or 'pending' when a multi-file torrent
   *  needs a tree review before it can be staged (sets `pending` itself). */
  async function sendOne(f: File): Promise<Outcome | 'pending'> {
    try {
      const tree = await parseTorrentUpload(f);
      if (tree.files.length <= 1) {
        // Nothing to choose - a tree with one row in it is the single-file
        // staging card by another name.
        return await commitTorrent(f.name, tree, tree.files.map(() => true));
      }
      setPending({ file: f.name, tree, selected: tree.files.map((x) => x.selected) });
      return 'pending';
    } catch {
      // Not a torrent (or genuinely not parseable as one) - try it as a
      // container instead of failing outright.
    }
    if (f.size > MAX_CONTAINER_BYTES) {
      return { file: f.name, kind: 'failed', reason: t('container.tooBig', { max: fmtBytes(MAX_CONTAINER_BYTES) }) };
    }
    try {
      const r = await uploadContainer(f, pkg);
      if (r.handedTo === 'jd') return { file: f.name, kind: 'container-handed', expiresIn: r.expiresIn };
      // The package is read off what was actually created rather than off the
      // package field, because a Packagizer rule may have overridden it;
      // several packages means there is no single one to name.
      const landed = new Set(r.created.map((c) => c.package).filter(Boolean));
      return {
        file: f.name,
        kind: 'container-staged',
        links: r.links,
        created: r.created.length,
        pkg: landed.size === 1 ? [...landed][0] : '',
      };
    } catch (e) {
      return { file: f.name, kind: 'failed', reason: message(e) };
    }
  }

  async function sendFiles(files: File[]) {
    // A second batch is refused while one is in flight, or while a torrent
    // tree is under review, rather than queued - the results block reports
    // one batch, and interleaving would leave a "12 links staged" line
    // standing next to a file it did not come from.
    if (!files.length || busy || pending) return;
    setBusy(true);
    setResults([]);
    const out: Outcome[] = [];
    let stopped = false;
    for (const f of files) {
      if (stopped) {
        // A tree review is now open - a second file dropped alongside the
        // first is named rather than silently dropped or silently queued
        // behind a review nobody has finished yet.
        out.push({ file: f.name, kind: 'failed', reason: t('torrent.onlyOne') });
        setResults([...out]);
        continue;
      }
      const r = await sendOne(f);
      if (r === 'pending') {
        stopped = true;
        continue;
      }
      out.push(r);
      setResults([...out]); // each file reports as it lands, not after the last one
    }
    setBusy(false);
  }

  return (
    <div className="flex flex-col gap-1.5">
      <input
        ref={input}
        type="file"
        hidden
        multiple
        accept={FILE_ACCEPT}
        onChange={(e) => {
          void sendFiles([...(e.target.files ?? [])]);
          // Cleared so picking the same file again still fires a change
          // event - otherwise a re-upload after fixing the JD backend
          // silently does nothing.
          e.target.value = '';
        }}
      />

      {busy && <p className="px-1 text-xs text-carbon-textMuted">{t('container.uploading')}</p>}

      {pending && (
        <TorrentTreeCard
          pending={pending}
          onChange={(selected) => setPending({ ...pending, selected })}
          onCancel={() => setPending(null)}
          onConfirm={() => void confirmPending()}
          busy={busy}
        />
      )}

      {results.map((o, i) => (
        <Result key={`${o.file}|${i}`} o={o} />
      ))}
    </div>
  );
});
