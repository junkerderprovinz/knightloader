import { useRef, useState } from 'react';
import { parseTorrentUpload, stageTorrent, type Task, type TorrentTree } from '../lib/api';
import { fmtBytes } from '../lib/format';
import { message } from '../lib/intake';
import { useT } from '../lib/i18n';
import { Button, InfoBubble } from './ui';
import { IconCheck, IconFolder } from '../lib/icons';

// What the file dialog offers. The server decides by content, not by name -
// torrent.LooksLikeTorrent reads the bencoded bytes, never the extension - so
// this is a convenience for the picker and never a gate, the same reasoning
// ContainerDrop's own CONTAINER_ACCEPT gives.
const TORRENT_ACCEPT = '.torrent';

/** One upload's answer, kept structured so the sentence is built at render
 *  time and follows a language change instead of freezing at upload time -
 *  mirrors ContainerDrop's own Outcome. */
type Outcome =
  | { file: string; kind: 'staged'; task: Task }
  | { file: string; kind: 'duplicate' }
  | { file: string; kind: 'failed'; reason: string };

function Result({ o }: { o: Outcome }) {
  const { t } = useT();
  if (o.kind === 'failed') {
    return (
      <p className="px-4 text-xs text-statusFail">{t('torrent.failed', { file: o.file, reason: o.reason })}</p>
    );
  }
  if (o.kind === 'duplicate') {
    return <p className="px-4 text-xs text-carbon-textSub">{t('torrent.duplicate', { file: o.file })}</p>;
  }
  return (
    <p className="px-4 text-xs text-statusOk">
      {o.task.package
        ? t('torrent.stagedIn', { file: o.file, pkg: o.task.package })
        : t('torrent.staged', { file: o.file })}
    </p>
  );
}

// Pending is the file-tree review step: a parsed .torrent (or, once it has
// more than one file, its tree) waiting for a person to check/uncheck files
// before staging continues - see docs/torrent-support.md's UI section, "a new
// step in the collector flow, not a retrofit of the existing single-file
// staging card". selected is indexed the same as tree.files.
interface Pending {
  file: string;
  tree: TorrentTree;
  selected: boolean[];
}

/**
 * TorrentFileRow is one line of the tree: the whole row is the control, not a
 * checkbox nested inside one. Copies columns.tsx's exported Checkbox mark as a
 * plain span rather than importing that component, for the exact reason
 * CollectorFacets.tsx's own FacetRow already does the same thing right next to
 * this page: two interactive elements would answer for one click.
 */
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
 * TorrentUpload takes the .torrent files people are handed instead of a
 * magnet link - a magnet needs none of this, it already stages through the
 * paste box AddLinksForm owns (internal/resolver/torrent.Resolver.Match
 * recognises the scheme). Parsing and staging are two separate requests
 * (lib/api.ts's parseTorrentUpload/stageTorrent): nothing is created until a
 * multi-file torrent's tree has been reviewed, so navigating away from a
 * half-reviewed tree leaves nothing behind either way.
 *
 * One row tall when idle, same footprint reasoning as ContainerDrop right
 * above it in the collector: the paste box is why people open this page.
 */
export function TorrentUpload({ pkg = '' }: { pkg?: string }) {
  const { t } = useT();
  const input = useRef<HTMLInputElement>(null);
  const [dragOver, setDragOver] = useState(false);
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<Outcome[]>([]);
  const [pending, setPending] = useState<Pending | null>(null);

  async function commit(file: string, tree: TorrentTree, selected: boolean[]) {
    setBusy(true);
    try {
      // undefined (every file kept) rather than the full path list when
      // nothing was unticked, matching stageTorrent's own documented default
      // and keeping a single-file torrent's request body trivially small.
      const selectedPaths = selected.every(Boolean) ? undefined : tree.files.filter((_, i) => selected[i]).map((f) => f.path);
      const task = await stageTorrent(tree.uri, pkg, selectedPaths);
      setResults((r) => [...r, task ? { file, kind: 'staged', task } : { file, kind: 'duplicate' }]);
    } catch (e) {
      setResults((r) => [...r, { file, kind: 'failed', reason: message(e) }]);
    } finally {
      setBusy(false);
      setPending(null);
    }
  }

  async function send(files: File[]) {
    if (!files.length || busy || pending) return;
    const [file, ...rest] = files;
    setBusy(true);
    setResults([]);
    try {
      const tree = await parseTorrentUpload(file);
      if (tree.files.length <= 1) {
        // Nothing to choose - decision 6 is about MULTI-file torrents, and a
        // tree with one row in it is the single-file staging card by another
        // name.
        await commit(file.name, tree, tree.files.map(() => true));
      } else {
        setPending({ file: file.name, tree, selected: tree.files.map((f) => f.selected) });
      }
    } catch (e) {
      setResults([{ file: file.name, kind: 'failed', reason: message(e) }]);
    } finally {
      setBusy(false);
    }
    if (rest.length) {
      // The tree step means only one upload can be under review at a time;
      // a second file dropped alongside the first is named rather than
      // silently dropped or silently queued behind a review nobody has
      // finished yet.
      setResults((r) => [...r, ...rest.map((f) => ({ file: f.name, kind: 'failed' as const, reason: t('torrent.onlyOne') }))]);
    }
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          void send([...e.dataTransfer.files]);
        }}
        className={`flex flex-wrap items-center gap-3 rounded-[var(--radius-control)] px-4 py-2 transition-colors ${
          dragOver ? 'bg-accentSoft shadow-[0_0_0_2px_var(--focus-ring)]' : 'bg-carbon-surface2'
        }`}
      >
        <span className="flex items-center text-xs text-carbon-textSub">
          {t('torrent.prompt')}
          <InfoBubble tip={t('torrent.info')} />
        </span>
        <span className="flex-1" />
        <Button
          kind="ghost"
          className="px-2.5 text-xs"
          icon={<IconFolder width={14} height={14} />}
          onClick={() => input.current?.click()}
          disabled={busy || !!pending}
        >
          {busy ? t('torrent.uploading') : t('torrent.choose')}
        </Button>
        <input
          ref={input}
          type="file"
          hidden
          accept={TORRENT_ACCEPT}
          onChange={(e) => {
            void send([...(e.target.files ?? [])]);
            // Cleared so picking the same file again still fires a change
            // event - the same reason ContainerDrop's own input does this.
            e.target.value = '';
          }}
        />
      </div>

      {pending && (
        <TorrentTreeCard
          pending={pending}
          onChange={(selected) => setPending({ ...pending, selected })}
          onCancel={() => setPending(null)}
          onConfirm={() => void commit(pending.file, pending.tree, pending.selected)}
          busy={busy}
        />
      )}

      {results.map((o, i) => (
        <Result key={`${o.file}|${i}`} o={o} />
      ))}
    </div>
  );
}
