// The collector's file intake for .torrent and link-container files
// (.txt/.dlc/.ccf/.rsdf), reached through AddLinksForm's picker button and
// paste box. A file is tried as a torrent first, which the server sniffs by
// content, and falls back to the container endpoint. The picker's `accept` is
// a convenience, not a gate.
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { parseTorrentUpload, stageTorrent, uploadContainer, type Task, type TorrentTree } from '../lib/api';
import { fmtBytes } from '../lib/format';
import { message } from '../lib/intake';
import { useT } from '../lib/i18n';
import { Button } from './ui';
import { Tip } from './columns';
import { ProgressBar } from './ProgressBar';
import { IconCheck } from '../lib/icons';

// fmtElapsed prints seconds in fmtEta's compact shape: "12s", "3m 5s", "1h 2m".
function fmtElapsed(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (s < 3600) return `${m}m ${s % 60}s`;
  const h = Math.floor(s / 3600);
  return `${h}h ${Math.floor((s % 3600) / 60)}m`;
}

/**
 * ContainerHandedProgress shows an indeterminate bar and elapsed time while an
 * encrypted container waits on the JD sidecar. It ends when a container link
 * lands in the collector, or when the relay's TTL runs out.
 *
 * The handover has no id to correlate against, so two containers dropped
 * together end on the first one's links. The rows sit in AddLinksForm's footer
 * slot, which supplies the padding.
 */
function ContainerHandedProgress({
  file,
  expiresIn,
  startedAt,
  landedAt,
  onExpire,
}: {
  file: string;
  expiresIn: number;
  /** Stamped when the handover response arrived; the server keeps no start. */
  startedAt: number;
  /** When a container link last landed in the collector, in epoch ms, or 0. */
  landedAt: number;
  onExpire: () => void;
}) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const elapsed = Math.max(0, Math.round((now - startedAt) / 1000));
  const landed = landedAt > startedAt;
  useEffect(() => {
    if (landed || elapsed >= expiresIn) onExpire();
  }, [landed, elapsed, expiresIn, onExpire]);
  return (
    <div className="flex flex-col gap-1.5">
      {/* The bar gets its own full-width line. */}
      <div className="flex items-baseline gap-2">
        <Tip dir="ltr" tip={file} className="min-w-0 flex-1 truncate text-xs text-carbon-textSub">
          {file}
        </Tip>
        <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">{fmtElapsed(elapsed)}</span>
      </div>
      <ProgressBar active percent={0} indeterminate />
    </div>
  );
}

/** What Collector.tsx reaches through the ref, for AddLinksForm's picker
 *  button and paste box drops. */
export interface FileDropHandle {
  openPicker: () => void;
  handleFiles: (files: File[]) => void;
}

const FILE_ACCEPT = '.torrent,.txt,.dlc,.ccf,.rsdf';

// Mirrors container.MaxBytes in internal/container.
const MAX_CONTAINER_BYTES = 8 << 20;

// Structured, so the sentence follows a language change after the upload.
type Outcome =
  | { file: string; kind: 'container-staged'; links: number; created: number; pkg: string }
  | { file: string; kind: 'container-handed'; expiresIn: number; startedAt: number }
  | { file: string; kind: 'torrent-staged'; task: Task }
  | { file: string; kind: 'torrent-duplicate' }
  | { file: string; kind: 'failed'; reason: string };

function Result({ o, landedAt, onExpire }: { o: Outcome; landedAt: number; onExpire: () => void }) {
  const { t } = useT();

  if (o.kind === 'failed') {
    return <p className="text-xs text-statusFail">{t('container.failed', { file: o.file, reason: o.reason })}</p>;
  }
  if (o.kind === 'torrent-duplicate') {
    return <p className="text-xs text-carbon-textSub">{t('torrent.duplicate', { file: o.file })}</p>;
  }
  if (o.kind === 'torrent-staged') {
    return (
      <p className="text-xs text-statusOk">
        {o.task.package ? t('torrent.stagedIn', { file: o.file, pkg: o.task.package }) : t('torrent.staged', { file: o.file })}
      </p>
    );
  }
  // Nothing is staged yet; the links arrive over the websocket later.
  if (o.kind === 'container-handed') {
    return (
      <ContainerHandedProgress
        file={o.file}
        expiresIn={o.expiresIn}
        startedAt={o.startedAt}
        landedAt={landedAt}
        onExpire={onExpire}
      />
    );
  }
  // Every link was already in the list.
  if (o.created === 0) {
    return <p className="text-xs text-carbon-textSub">{t('container.allKnown', { file: o.file, n: o.links })}</p>;
  }
  const known = o.links - o.created;
  return (
    <p className="text-xs text-statusOk">
      {o.pkg ? t('container.stagedIn', { n: o.created, file: o.file, pkg: o.pkg }) : t('container.staged', { n: o.created, file: o.file })}
      {known > 0 && ` ${t('container.alsoKnown', { n: known })}`}
    </p>
  );
}

// Pending is a multi-file torrent awaiting file selection before staging;
// selected is indexed like tree.files.
interface Pending {
  file: string;
  tree: TorrentTree;
  selected: boolean[];
}

// TorrentFileRow is one line of the tree; the row itself is the control, as in
// CollectorFacets.tsx's FacetRow.
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
      <Tip dir="ltr" tip={path} className="min-w-0 flex-1 truncate text-start">
        {path}
      </Tip>
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
    // A well, since this renders inside AddLinksForm's card.
    <div className="glim-well flex flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <Tip tip={pending.tree.name} className="block truncate text-sm font-medium text-carbon-text">
            {pending.tree.name}
          </Tip>
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
        <button type="button" className="text-accentInk hover:underline" onClick={() => setAll(true)}>
          {t('torrent.tree.selectAll')}
        </button>
        <button type="button" className="text-accentInk hover:underline" onClick={() => setAll(false)}>
          {t('torrent.tree.selectNone')}
        </button>
      </div>

      {/* Hairlines rather than a fill, which would vanish on the surface2 well. */}
      <div className="max-h-64 divide-y divide-carbon-border/60 overflow-y-auto rounded-[var(--radius-control)]">
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
 * FileDrop handles files handed over through its ref and renders only the
 * torrent review card and a batch's outcome lines.
 */
export const FileDrop = forwardRef<FileDropHandle, { pkg?: string; landedAt?: number }>(function FileDrop(
  { pkg = '', landedAt = 0 },
  ref,
) {
  const { t } = useT();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [results, setResults] = useState<Outcome[]>([]);
  const [pending, setPending] = useState<Pending | null>(null);

  useImperativeHandle(ref, () => ({
    // Same guard as sendFiles, so the picker does not open when its files
    // would be refused.
    openPicker: () => {
      if (!busy && !pending) input.current?.click();
    },
    handleFiles: (files: File[]) => void sendFiles(files),
  }));

  async function commitTorrent(file: string, tree: TorrentTree, selected: boolean[]): Promise<Outcome> {
    try {
      // undefined keeps every file, stageTorrent's default.
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

  // sendOne tries a file as a torrent, then as a container. It returns
  // 'pending' after opening the review for a multi-file torrent.
  async function sendOne(f: File): Promise<Outcome | 'pending'> {
    try {
      const tree = await parseTorrentUpload(f);
      if (tree.files.length <= 1) {
        return await commitTorrent(f.name, tree, tree.files.map(() => true));
      }
      setPending({ file: f.name, tree, selected: tree.files.map((x) => x.selected) });
      return 'pending';
    } catch {
      // Not a torrent; try it as a container.
    }
    if (f.size > MAX_CONTAINER_BYTES) {
      return { file: f.name, kind: 'failed', reason: t('container.tooBig', { max: fmtBytes(MAX_CONTAINER_BYTES) }) };
    }
    try {
      const r = await uploadContainer(f, pkg);
      if (r.handedTo === 'jd') {
        return { file: f.name, kind: 'container-handed', expiresIn: r.expiresIn, startedAt: Date.now() };
      }
      // Read from the created tasks, since a Packagizer rule may have renamed it.
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
    // Refused rather than queued, since the results report one batch.
    if (!files.length || busy || pending) return;
    setBusy(true);
    setResults([]);
    const out: Outcome[] = [];
    let stopped = false;
    for (const f of files) {
      if (stopped) {
        // A review is open, so the remaining files are reported as refused.
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
      setResults([...out]);
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
          // Cleared so picking the same file again fires a change event.
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
        <Result
          key={`${o.file}|${i}`}
          o={o}
          landedAt={landedAt}
          onExpire={() => setResults((r) => r.filter((x) => x !== o))}
        />
      ))}
    </div>
  );
});
