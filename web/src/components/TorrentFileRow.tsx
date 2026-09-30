import { useCallback, useEffect, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { fetchTorrentFiles, selectTorrentFiles, type Task, type TorrentFileView } from '../lib/api';
import { hueVars } from '../lib/appearance';
import { fmtBytes } from '../lib/format';
import { useToast } from '../lib/toast';
import { ProgressCell, RowSwitch, Tip, TREE_INDENT, TWISTY_STEP, type CellContext, type ColumnDef } from './columns';
import type { ListRow } from './listRows';

// A torrent's files under its row. The task carries only how many there are
// (torrentFileCount), because a torrent can list thousands and the task goes
// out on every websocket tick; the list itself is asked for while the row is
// open, and asked again every POLL_MS while the torrent downloads, since that
// is when each file's count moves.

const POLL_MS = 2000;

/** Whether a task is a torrent whose files are worth a row each: a single
 *  file is the row itself. */
export function hasTorrentFiles(task: Task): boolean {
  return (task.torrentFileCount ?? 0) > 1;
}

/**
 * loneTorrent is the torrent a package consists of when the package holds that
 * one link and bears its name, or the package is the unnamed one. Such a
 * package is the torrent's own folder a second time, so the list draws the
 * torrent as the package header and its files straight under it.
 */
export function loneTorrent(name: string, items: readonly Task[]): Task | undefined {
  if (items.length !== 1 || !hasTorrentFiles(items[0])) return undefined;
  const task = items[0];
  const same = name === '' || name.trim().toLowerCase() === (task.name || '').trim().toLowerCase();
  return same ? task : undefined;
}

/** A finished torrent, whose files can only be read. */
export function torrentFinished(task: Task): boolean {
  return task.status === 'done' || task.status === 'extracting' || !!task.seeding;
}

/**
 * useTorrentFiles holds the file lists of the open torrents in `open`. A list
 * is asked for again when its torrent's state or size changes, which is what a
 * change of files or a finish looks like on the task, and on a timer while it
 * runs. A list nobody could fetch stays away, so a peer on an older build
 * shows its torrents without files rather than an error on every tick.
 */
export function useTorrentFiles(open: ReadonlySet<string>, byId: ReadonlyMap<string, Task>, base: string) {
  const [lists, setLists] = useState<ReadonlyMap<string, TorrentFileView[]>>(() => new Map());
  const shown = [...open].flatMap((id) => {
    const t = byId.get(id);
    return t && hasTorrentFiles(t) ? [t] : [];
  });
  const stateKey = shown
    .map((t) => `${t.id}:${t.status}:${t.size}:${t.torrentFileCount}:${t.seeding ? 1 : 0}`)
    .join(',');
  const runningKey = shown
    .filter((t) => t.status === 'running')
    .map((t) => t.id)
    .join(',');

  const put = useCallback((id: string, files: TorrentFileView[]) => {
    setLists((m) => new Map(m).set(id, files));
  }, []);

  useEffect(() => {
    let live = true;
    const load = (ids: string[]) => {
      for (const id of ids) {
        fetchTorrentFiles(id, base).then(
          (files) => {
            if (live) put(id, files);
          },
          () => undefined,
        );
      }
    };
    load(stateKey ? stateKey.split(',').map((s) => s.slice(0, s.indexOf(':'))) : []);
    const timer = runningKey ? window.setInterval(() => load(runningKey.split(',')), POLL_MS) : 0;
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [stateKey, runningKey, base, put]);

  return { lists, put };
}

/**
 * TorrentFileRow is one file of an open torrent: its path inside the torrent,
 * its size, how much of it is here and its switch. It is a row of the tree and
 * not a task, so a press on it selects nothing and it never travels in a move
 * on its own; it slides and lifts with its torrent's row, whose look and slide
 * it is handed.
 *
 * The switch sits in the Enabled column where the collector draws one, beside
 * the links' own switches, and at the row's end everywhere else. A finished
 * torrent has none: nothing it could change would be fetched.
 */
export function TorrentFileRow({
  row,
  siblings,
  base,
  ctx,
  columns,
  current,
  onKeyDown,
  onChanged,
  look,
  slide,
}: {
  row: Extract<ListRow, { kind: 'file' }>;
  /** Every file of the torrent, which the switch sends as the new choice. */
  siblings: TorrentFileView[];
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  current: boolean;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
  onChanged: (files: TorrentFileView[]) => void;
  look: string;
  slide: CSSProperties;
}) {
  const { t } = ctx;
  const { toast } = useToast();
  const { task, file } = row;
  const [busy, setBusy] = useState(false);
  const [shake, setShake] = useState(0);
  const finished = torrentFinished(task);

  async function flip() {
    if (busy) return;
    const paths = siblings.filter((f) => (f.path === file.path ? !file.selected : f.selected)).map((f) => f.path);
    if (paths.length === 0) {
      toast(t('task.torrentFile.lastOne'), 'info');
      setShake((n) => n + 1);
      return;
    }
    setBusy(true);
    try {
      onChanged(await selectTorrentFiles(task.id, paths, base));
    } catch (err) {
      toast(err instanceof Error && err.message ? err.message : t('task.switchFailed'), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  const toggle = finished ? null : (
    <RowSwitch
      on={file.selected}
      label={t(file.selected ? 'task.torrentFile.skip' : 'task.torrentFile.fetch')}
      busy={busy}
      shake={shake}
      onFlip={() => void flip()}
      tabIndex={current ? 0 : -1}
    />
  );
  const inColumn = columns.some((c) => c.id === 'enabled');

  function cell(col: ColumnDef) {
    switch (col.id) {
      case 'enabled':
        return toggle;
      case 'name':
        return (
          <Tip
            tip={file.path}
            dir="ltr"
            className={`block truncate text-start text-sm ${file.selected ? 'text-carbon-textSub' : 'text-carbon-textMuted'}`}
          >
            {file.path}
          </Tip>
        );
      case 'size':
        return fmtBytes(file.size);
      case 'progress':
        return file.done === undefined ? null : (
          <ProgressCell
            loaded={file.done}
            size={file.size}
            done={file.done >= file.size}
            active
            live={task.status === 'running'}
          />
        );
      default:
        return null;
    }
  }

  return (
    <div
      // What the window measures and what the keyboard focuses, and nothing a
      // task is looked up by: no data-task-id, so a right-click here acts on
      // the list and not on the torrent (see ListToolbar's targetTaskId). The
      // drag reads data-file-of to count this row into its torrent's box.
      data-row-key={row.key}
      data-row-kind="file"
      data-file-of={task.id}
      data-file-under={row.under}
      role="treeitem"
      tabIndex={current ? 0 : -1}
      aria-level={row.level}
      aria-posinset={row.posinset}
      aria-setsize={row.setsize}
      onKeyDown={onKeyDown}
      style={{ ...hueVars(row.index), gridTemplateColumns: 'var(--kl-cols)', ...slide } as CSSProperties}
      className={`glim-hue glim-tint select-none ${look} relative grid items-center px-3 py-0.5 transition-colors
        hover:bg-carbon-hover/50 has-[:focus-visible]:bg-carbon-hover/50`}
    >
      {columns.map((col) => {
        const node = cell(col);
        return (
          <div
            key={col.id}
            dir={col.ltr ? 'ltr' : undefined}
            // One level below its torrent: the torrent's twisty hangs in front
            // of the torrent's name, and the files start where the name does
            // plus that step. Under a torrent that is its package's header they
            // start where a link inside a package does.
            style={
              col.id === 'name'
                ? { paddingInlineStart: `${row.level === 2 ? TREE_INDENT : TREE_INDENT + TWISTY_STEP}px` }
                : undefined
            }
            className={`min-w-0 truncate text-xs text-carbon-textSub ${col.id === 'name' ? 'pe-2' : 'px-2'} ${
              col.align === 'end' ? 'text-end' : col.align === 'center' ? 'text-center' : 'text-start'
            } ${col.numeric ? 'glim-num' : ''}`}
          >
            {typeof node === 'string' ? (
              <Tip tip={node} className="block truncate">
                {node}
              </Tip>
            ) : (
              node
            )}
          </div>
        );
      })}
      {/* As tall as a link row's badges, so every row of the tree is one height. */}
      <div className="flex min-h-[var(--btn-h)] items-center justify-end gap-1">{inColumn ? null : toggle}</div>
    </div>
  );
}
