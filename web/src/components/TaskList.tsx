import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type DragEvent,
  type PointerEvent,
} from 'react';
import { createPortal } from 'react-dom';
import {
  priorityChoices,
  type PriorityChoice,
  type Task,
  type TaskOptionsPatch,
  type YtdlpHosterPreset,
  YTDLP_VARIANT_KINDS,
  fetchHosterPreset,
  fetchOptions,
  saveHosterPreset,
} from '../lib/api';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import { pause, resume, remove, startTasks, restartTasks, recheckTasks, setTaskOptions, reorderTasks } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useUIState } from '../lib/uistate';
import { Button, Card, Field, FieldGroup, IconBadge, InfoBubble, Modal, SectionTitle, TextArea, TextInput } from './ui';
import { Tabs } from './Tabs';
import { ColumnMenu } from './ColumnMenu';
import {
  COLUMN_BY_ID,
  Checkbox,
  FOLDER_GLYPH,
  TREE_INDENT,
  VARIANT_KIND_LABEL_KEY,
  applySort,
  gridTemplate,
  moveColumn,
  nextSort,
  resolveLayout,
  sharedPriority,
  toStored,
  usePriorityNames,
  variantKindOf,
  PriorityTag,
  type CellContext,
  type ColumnDef,
  type ColumnId,
  type ColumnLayout,
  type ListProfile,
  type ResolvedLayout,
  type SortState,
} from './columns';
import {
  IconPause,
  IconPlay,
  IconTrash,
  IconRetry,
  IconFolder,
  IconSearch,
  IconSettings,
  IconArrowUp,
  IconArrowDown,
} from '../lib/icons';

export interface Selection {
  ids: Set<string>;
  toggle: (id: string) => void;
  /** Replaces the whole selection outright - the write path a plain click
   *  and a Shift-range both need (TaskListCard's own selectUnit), which a
   *  per-id toggle cannot express without the caller reconstructing the
   *  diff itself. */
  set: (ids: Set<string>) => void;
}

// A stable empty default: useUIState leaves the fallback out of its dependencies
// on purpose, and handing it a fresh [] on every render would make every
// subscriber think the value changed.
const NO_COLLAPSED: string[] = [];

// Every row is the same grid, and the track list reaches it through one custom
// property set on the table. That is what lets a column drag repaint by touching
// a single element instead of re-rendering several hundred rows per pointer move.
const ROW_GRID: CSSProperties = { gridTemplateColumns: 'var(--kl-cols)' };

// What a row's drag style is when no drag is in flight: nothing at all.
//
// EMPTY rather than `{ transform: 'none', transition: 'none' }`, because React
// clears an inline style property by seeing it DISAPPEAR from the style object;
// spelling out 'none' would leave both properties on the element for the rest of
// the session and quietly override whatever the stylesheet has to say about
// them. Shared constants so that neither the empty style nor the empty map is
// rebuilt for every row on every render of a list several hundred rows long.
const NO_SLIDE: CSSProperties = {};
const NO_OFFSETS = new Map<string, number>();

/**
 * useCollapsedPackages is the folded set, and the only thing that knows where it
 * is kept.
 *
 * The list card holds it and so does the page, because the right-click menu
 * folds packages too and the menu belongs to the page. Both read the same field
 * of the same store, which notifies every subscriber on write — so the twisty
 * and the menu entry can never disagree about what is open.
 *
 * Folded packages are keyed by name, which is the only identity the wire model
 * carries: core.Task has a package name and no package id. SetPackage rewrites
 * the name, so a rename or a Packagizer re-package makes a folded package come
 * back expanded. That is the mild failure of the two — the harmful one would be
 * pruning names that are not on screen, which would unfold every package the
 * search is currently hiding — but it needs a stable package id on the task to
 * fix properly, and that is the model owner's lane, not this file's.
 */
export function useCollapsedPackages(profile: ListProfile = 'downloads') {
  const [stored, setStored] = useUIState<string[]>(`list.collapsed.${profile}`, NO_COLLAPSED);
  const collapsed = useMemo(() => new Set(stored), [stored]);

  const collapse = useCallback(
    (names: string[]) => {
      const next = new Set(stored);
      for (const n of names) next.add(n);
      setStored([...next]);
    },
    [stored, setStored],
  );

  // Named, never "clear the lot": expanding what is on screen must not unfold
  // the packages a search is currently hiding.
  const expand = useCallback(
    (names: string[]) => {
      const next = new Set(stored);
      for (const n of names) next.delete(n);
      setStored([...next]);
    },
    [stored, setStored],
  );

  const toggle = useCallback(
    (name: string) => {
      if (collapsed.has(name)) expand([name]);
      else collapse([name]);
    },
    [collapsed, collapse, expand],
  );

  return { collapsed, collapse, expand, toggle };
}

/**
 * The tree control, as a filled triangle rather than a chevron.
 *
 * Swing's JTree draws exactly this, which is what a package row looks like in
 * JDownloader, and it is the one mark on the row that says "there is something
 * inside this". It points along the reading direction when shut and downward
 * when open, so in an Arabic or Hebrew interface it points the way that
 * interface reads.
 */
function Twisty({ open }: { open: boolean }) {
  const rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl';
  return (
    <svg viewBox="0 0 16 16" width={11} height={11} aria-hidden focusable="false">
      <path
        d="M5.5 2.8 11.6 8l-6.1 5.2z"
        fill="currentColor"
        style={{
          transformOrigin: '8px 8px',
          transform: open ? 'rotate(90deg)' : rtl ? 'rotate(180deg)' : 'none',
        }}
      />
    </svg>
  );
}

function TaskRow({
  task,
  base,
  ctx,
  columns,
  selection,
  index,
  dnd,
  onSelect,
  onOpenProperties,
}: {
  task: Task;
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  selection?: Selection;
  /** Position in the rendered list — the rainbow palette position. */
  index: number;
  /** The row drag-to-reorder machinery — see TaskListCard, the one place it is built. */
  dnd: RowDnD;
  /** Click-to-select (TaskListCard's own selectUnit) - reads the click's own
   *  modifier keys, so this row does not have to know Ctrl/Shift's meaning
   *  itself. Absent wherever selection is (Downloads.tsx renders no
   *  checkbox column and takes no selection prop today either). */
  onSelect?: (e: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }) => void;
  /** Opens the properties panel for whatever a single click just selected
   *  (jdp, 2026-08-26: "wenn man einmal auf einen link oder einen ordner
   *  klickt kommt sofort die eigenschaften card. die soll erst erscheinen
   *  bei doppelklick" - a plain click used to open it immediately as a
   *  side effect of selecting anything at all, which made a quick
   *  multi-select impossible without the panel flashing open and shut on
   *  every intermediate click). The double-click's own leading single
   *  click already ran onSelect above by the time this fires - no
   *  modifier keys to read here, only "show it now". */
  onOpenProperties?: () => void;
}) {
  const { t } = useT();
  const collected = task.status === 'collected';
  const settled = task.status === 'done' || task.status === 'error';
  const dragging = dnd.draggingTask === task.id;

  // In rainbow mode the row owns a colour, and everything inside it that paints
  // activity — the progress fill above all — reads it through --accent without
  // knowing the mode exists. A running row counts as active, so the reactive
  // reading still shows colour where work is actually happening.
  //
  // The colour comes from the row's position, not from a hash of its id. A hash
  // is stable when rows above finish, which sounds better until three rows land
  // in the same bucket and two neighbours share a colour — which is the one
  // thing the mode exists to prevent. By position, eight adjacent rows always
  // differ.
  return (
    <div
      // What a right-click landed on. Without it the menu can only ever act on
      // whatever happened to be selected already, which is how the wrong
      // download gets deleted.
      data-task-id={task.id}
      // The live drag preview moves this row by SLIDING it (a transform), not by
      // rendering it somewhere else in the list - see TaskListCard's own
      // previewOffsets. While a drag is in flight this carries a translateY and a
      // transform transition; with no drag the two properties are simply absent
      // again and the row sits where the document flow puts it.
      style={
        { ...hueVars(rainbowAt(index)), ...ROW_GRID, ...dnd.slide({ kind: 'task', id: task.id }) } as CSSProperties
      }
      // Drag-to-reorder, on the same native HTML5 machinery the column
      // headers already use above (Header's own dragId/onDragStart/onDrop).
      // Only offered in queue-order view — see dndEnabled in TaskListCard.
      // The CONTROL guard is the same one the package header below already
      // folds by (jdp: everything that is its own control keeps its own
      // gesture) — reused here so a drag never starts out from under the
      // checkbox or an action badge.
      draggable={dnd.enabled}
      onDragStart={(e) => {
        if (!dnd.enabled || (e.target instanceof Element && e.target.closest(CONTROL))) {
          e.preventDefault();
          return;
        }
        dnd.startTask(task.id);
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', task.id);
      }}
      onDragEnd={dnd.end}
      onDragOver={(e) => {
        if (!dnd.active) return;
        e.preventDefault();
        dnd.previewOverTask(task.id, e);
      }}
      onDrop={(e) => dnd.dropOnTask(task.id, e)}
      // Click-to-select (jdp, 2026-08-26: "in der linkliste soll man links
      // und ordner mit einem klick markieren können, nicht den ordner
      // aufklappen... mehrere links oder ordner soll man mit klick und
      // strg oder umschalttaste auswählen können. wie in windows") -
      // replaces the checkbox column this row used to carry. Guarded by
      // CONTROL the same way PackageGroup's own row click already is, so
      // clicking an action badge or the Enabled switch acts on that
      // control instead of also selecting the row underneath it.
      onClick={(e) => {
        if (e.target instanceof Element && e.target.closest(CONTROL)) return;
        onSelect?.(e);
      }}
      onDoubleClick={(e) => {
        if (e.target instanceof Element && e.target.closest(CONTROL)) return;
        onOpenProperties?.();
      }}
      // select-none, only while a drag is actually possible: without it, a
      // real mouse press-and-drag that starts over the row's own text (the
      // name or URL column - the columns a hand naturally lands on) is read
      // by the browser as starting a text selection instead of the native
      // HTML5 drag, so draggable="true" never gets as far as firing
      // dragstart at all. The package header beside this row already has
      // this for the same reason (its own onClick needs the identical
      // guard) - this row was the one place it had been missed. Left
      // selectable when dnd is off (a sorted view) since nothing here
      // competes with it then.
      // bg-accent/20, not the softer bg-accentSoft token this used at first
      // (jdp, 2026-08-26: "wenn eine zeile ausgewählt ist erkennt man das
      // nicht" - accentSoft is 14% alpha, chosen for a hover/drag hint that
      // is meant to stay quiet, and a selected row wants the opposite: a
      // mark somebody actually notices). A real background-color layered
      // over glim-tint's own inset box-shadow rainbow wash rather than
      // fighting it for the same CSS property, so both show at once.
      className={`glim-hue glim-tint ${dnd.enabled ? 'select-none' : ''} ${task.status === 'running' ? 'glim-active' : ''} ${dragging ? 'opacity-50' : ''} ${
        selection?.ids.has(task.id) ? 'bg-accent/20' : ''
      } group relative grid
        items-center px-3 py-2 transition-colors hover:bg-carbon-hover/50`}
    >
      {columns.map((col) => {
        const node = col.render(task, ctx);
        return (
          <div
            key={col.id}
            dir={col.ltr ? 'ltr' : undefined}
            // A column that renders plain text gets that text as its native
            // tooltip, so a name too long for its width is still readable
            // without widening the column first.
            title={typeof node === 'string' ? node : undefined}
            // The name cell is the tree column, so a link inside a package is
            // indented under it — the second half of what makes the header above
            // read as a container rather than as another row in bold. Written as
            // its own padding pair rather than as `px-2 ps-6`, because two
            // utilities setting the same edge leave the result to stylesheet
            // order.
            //
            // TREE_INDENT, not a guess: it is the exact width of the package
            // row's leading furniture (see columns.tsx), so a link's name starts
            // where its package's name starts. At `ps-9` it started 24px BEFORE
            // it — measured on the live instance — which reads as the link being
            // the outer level and the package the inner one, i.e. the tree
            // upside down.
            style={col.id === 'name' ? { paddingInlineStart: `${TREE_INDENT}px` } : undefined}
            className={`min-w-0 truncate text-[12.5px] text-carbon-textSub ${
              col.id === 'name' ? 'pe-2' : 'px-2'
            } ${col.align === 'end' ? 'text-end' : col.align === 'center' ? 'text-center' : 'text-start'} ${col.numeric ? 'glim-num' : ''}`}
          >
            {node}
          </div>
        );
      })}

      {/* The primary action stays visible; the rest appears on hover or focus,
          so a long list reads as content instead of a wall of buttons.
          IconBadge, not a plain ghost icon (jdp, on the same pattern in
          Rules.tsx: "die icons ... sind nicht im Glimstone. das sollen
          farbige quadratischen badges mit icon sein") - this is the
          highest-traffic row in the app, so it gets the fix first. */}
      {/* Hued as of jdp, 2026-08-25: "alle quadratischen icons in die
          farbmodie bzw die farbengine aufnehmen. auch die überhalb des
          hauptfensters" - a Kurswechsel from this section's own earlier
          reasoning (the row already owns one rainbow position via
          glim-hue/glim-tint above, and colouring several DIFFERENT-job
          badges with small repeating indices would read as a second,
          competing colour layer rather than this row's own colour). Kept
          per SLOT rather than per row-position, so every row's own
          play/pause/resume badge is always the same hue and Folder is
          always the next one after it, the same "the position is the
          identity" rule Look.tsx's own colour swatches and every other
          badge SET in this app already follow - not a hash of the task id,
          which would repaint a badge a different colour every time its own
          row moved. Trash itself takes a hue too now (jdp, 2026-08-25:
          "der löschen badge soll nie anders eingfärbt sein... der soll
          ganz normal eingefärbt sein") - a second Kurswechsel, reversing
          this file's own earlier "destructive action keeps its own
          semantic red" choice: the badge that stood out in solid red next
          to its now-hued, at-rest-neutral siblings was read as the actual
          inconsistency, not the fix. */}
      <div className="flex items-center justify-end gap-1">
        {collected && (
          <IconBadge
            hue={0}
            icon={<IconPlay width={16} height={16} />}
            title={t('task.start')}
            aria-label={t('task.start')}
            onClick={() => startTasks([task.id], base)}
          />
        )}
        {task.status === 'running' && (
          <IconBadge
            hue={0}
            icon={<IconPause width={16} height={16} />}
            title={t('task.pause')}
            aria-label={t('task.pause')}
            onClick={() => pause(task.id, base)}
          />
        )}
        {task.status === 'paused' && (
          <IconBadge
            hue={0}
            icon={<IconPlay width={16} height={16} />}
            title={t('task.resume')}
            aria-label={t('task.resume')}
            onClick={() => resume(task.id, base)}
          />
        )}
        <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          {collected && (
            <IconBadge
              hue={1}
              icon={<IconSearch width={16} height={16} />}
              title={t('task.recheck')}
              aria-label={t('task.recheck')}
              onClick={() => recheckTasks([task.id], base)}
            />
          )}
          {/* No folder badge here any more (jdp, 2026-09-05). The dialog it
              opened is still one right-click away on every row, and the row's
              own hover strip is the wrong place for a setting: the other
              badges here DO something to this link, while that one only opened
              a form about it. The dialog itself is unchanged and still reached
              from the row menu (taskMenuGroups' own onOptions). */}
          {settled && (
            <IconBadge
              hue={3}
              icon={<IconRetry width={16} height={16} />}
              title={t('task.restart')}
              aria-label={t('task.restart')}
              onClick={() => restartTasks([task.id], base)}
            />
          )}
          <IconBadge
            hue={4}
            icon={<IconTrash width={16} height={16} />}
            title={t('task.remove')}
            aria-label={t('task.remove')}
            onClick={() => remove(task.id, base)}
          />
        </div>
      </div>

    </div>
  );
}

/**
 * The package header's name cell — the folder, the tree control, the name and
 * the file count, which together are what makes a package look like a package
 * rather than like a row somebody made bold.
 *
 * The online figure is a ratio and never a pair of counts: "3 of 5 online" is
 * true while a check is still running, whereas "3 online, 2 offline" claims the
 * other two were asked and said no.
 */
function PackageName({
  name,
  items,
  collapsed,
  onToggle,
}: {
  name: string;
  items: Task[];
  collapsed: boolean;
  onToggle: () => void;
}) {
  const { t } = useT();
  const priorityNames = usePriorityNames();
  const done = items.filter((x) => x.status === 'done').length;
  const label = t(collapsed ? 'task.expand' : 'task.collapse');

  const count = `${items.length} ${items.length === 1 ? t('task.file') : t('task.files')}${
    done > 0 ? ` · ${done} ${t('overview.done').toLowerCase()}` : ''
  }`;

  return (
    // @container, because what follows the name is whole or gone — never
    // shredded. `truncate` is the right tool for a value that still means
    // something cut short, and the wrong one for a two-word label: at a narrow
    // name column "2 Dateien" rendered as "2 D…", which is not information, it
    // is damage. Sized against this row rather than the viewport, since the name
    // column is dragged and hidden independently of the window.
    <div className="@container flex min-w-0 items-center gap-1.5">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={!collapsed}
        aria-label={label}
        title={label}
        className="grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-control)] text-carbon-textSub
          transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
      >
        <Twisty open={!collapsed} />
      </button>
      {/* Furniture, never the accent: every package has one, and a column of
          gold folders would spend the one colour that means "something is
          happening here" on the most ordinary fact on the page. */}
      <IconFolder
        width={FOLDER_GLYPH}
        height={FOLDER_GLYPH}
        className="shrink-0 text-carbon-textMuted"
      />
      {/* The package wears the mark when every link in it agrees, the same
          rule the Enabled column's own aggregate follows ("a package is on
          when every link in it is"). Without this the mark exists only on
          expanded rows, and a collapsed package - which is how a list of six
          packages is normally read - would hide the very thing it is there to
          announce. Rows that disagree show nothing here and keep their own
          marks inside. */}
      <PriorityTag value={sharedPriority(items)} names={priorityNames} t={t} />
      {/* The name wins the room. Everything after it is shrinkable and the name
          is not, below its own floor: with the counts pinned instead, a package
          called "Season One" in a 200px column rendered as "S · 3 files", which
          is the one word on the row nobody can do without. */}
      <span
        title={`${name || t('task.ungrouped')} - ${count}`}
        className="min-w-[5rem] flex-1 truncate text-[13.5px] font-semibold text-carbon-text"
      >
        {name || t('task.ungrouped')}
      </span>
      {/* The count hangs on the name's title as well, so a column too narrow to
          show it has not hidden anything that cannot be got at. Its own online
          ratio used to print here too ("5/5 online") - removed (jdp,
          2026-08-26: "wenn der statuspunkt grün ist sind ja alle online" -
          the package's own aggregate dot in the Status column, colours
          it exactly that already; printing the same fact a second time in
          words was the one column where a package read differently from
          its own status cell). */}
      <span className="glim-num hidden shrink-0 whitespace-nowrap text-[11px] text-carbon-textSub @[13rem]:inline">
        {count}
      </span>
    </div>
  );
}

/**
 * The gear badge a yt-dlp-routed package's header carries (jdp, 2026-08-25:
 * "auf dem link-ordner soll ein zahnrad-badge sein der mich zu den
 * voreinstellungen des Hosters führt") - it opens that link's own host's
 * "Variante" preset: which of the five rows (video/audio/thumbnail/
 * subtitle/description) a NEW link from this host starts with enabled, and
 * the default quality/audio format those rows start on. Not per-package -
 * per-HOST (GET/POST /api/ytdlp/preset), the same as every other link from
 * the same site, so a package with more than one host shows the badge for
 * whichever host its own "Variante" rows actually share.
 */
function HosterPresetButton({ host, base }: { host: string; base: string }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const label = `${t('collector.hosterPreset')} · ${host}`;
  return (
    <>
      <IconBadge
        hue={0}
        icon={<IconSettings width={16} height={16} />}
        title={label}
        aria-label={label}
        onClick={() => setOpen(true)}
      />
      {open &&
        createPortal(<HosterPresetDialog host={host} base={base} onClose={() => setOpen(false)} />, document.body)}
    </>
  );
}

// No useToast here, deliberately: this dialog reports a failed save inline
// (setError, rendered in the footer) and reports a successful one by closing.
// It held an unused `toast` for a while, found by a noUnusedLocals sweep -
// worth stating rather than silently deleting, so the next reader does not
// re-add it thinking the success path is missing its feedback.
function HosterPresetDialog({ host, base, onClose }: { host: string; base: string; onClose: () => void }) {
  const { t } = useT();
  const [preset, setPreset] = useState<YtdlpHosterPreset | null>(null);
  const [qualities, setQualities] = useState<string[]>([]);
  const [audioFormats, setAudioFormats] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let live = true;
    void Promise.all([fetchHosterPreset(host, base), fetchOptions()]).then(
      ([p, o]) => {
        if (!live) return;
        setPreset(p);
        setQualities(o.ytdlpQualities ?? []);
        setAudioFormats(o.ytdlpAudioFormats ?? []);
      },
      (err) => {
        if (live) setError(err instanceof Error && err.message ? err.message : String(err));
      },
    );
    return () => {
      live = false;
    };
  }, [host, base]);

  function toggleVariant(kind: (typeof YTDLP_VARIANT_KINDS)[number]) {
    setPreset((p) => {
      if (!p) return p;
      const on = p.variants.includes(kind);
      return { ...p, variants: on ? p.variants.filter((v) => v !== kind) : [...p.variants, kind] };
    });
  }

  async function save() {
    if (!preset) return;
    setSaving(true);
    try {
      await saveHosterPreset(host, preset, base);
      onClose();
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title={`${t('collector.hosterPreset')} · ${host}`}
      onClose={onClose}
      footer={
        <>
          <Button onClick={() => void save()} disabled={!preset || saving}>
            {t('settings.save')}
          </Button>
          {error && <span className="text-sm text-statusFail">{error}</span>}
        </>
      }
    >
      {!preset ? (
        <p className="text-sm text-carbon-textMuted">{t('common.loading')}</p>
      ) : (
        <>
          <p className="text-sm text-carbon-textMuted">{t('collector.hosterPresetIntro', { host })}</p>

          <FieldGroup label={t('columns.variant')}>
            <div className="flex flex-col gap-1.5">
              {YTDLP_VARIANT_KINDS.map((kind) => {
                const kindLabel = t(VARIANT_KIND_LABEL_KEY[kind] ?? VARIANT_KIND_LABEL_KEY.video);
                return (
                  <div key={kind} className="flex items-center gap-2 text-sm text-carbon-text">
                    <Checkbox
                      checked={preset.variants.includes(kind)}
                      label={kindLabel}
                      onChange={() => toggleVariant(kind)}
                    />
                    <span className="cursor-pointer" onClick={() => toggleVariant(kind)}>
                      {kindLabel}
                    </span>
                  </div>
                );
              })}
            </div>
          </FieldGroup>

          {qualities.length > 0 && (
            <FieldGroup label={t('settings.resolvers.quality')}>
              <Tabs
                size="sm"
                className="w-fit"
                label={t('settings.resolvers.quality')}
                active={preset.quality}
                onSelect={(id) => setPreset((p) => (p ? { ...p, quality: id } : p))}
                items={qualities.map((q) => ({ id: q, label: q }))}
              />
            </FieldGroup>
          )}

          {audioFormats.length > 0 && (
            <FieldGroup label={t('collector.hosterPresetAudioFormat')}>
              <Tabs
                size="sm"
                className="w-fit"
                label={t('collector.hosterPresetAudioFormat')}
                active={preset.audioFormat}
                onSelect={(id) => setPreset((p) => (p ? { ...p, audioFormat: id } : p))}
                items={audioFormats.map((f) => ({ id: f, label: f }))}
              />
            </FieldGroup>
          )}
        </>
      )}
    </Modal>
  );
}

// Anything that is itself a control keeps its own click. Everything else on a
// package header folds it, which is what people try first and what JDownloader
// does.
const CONTROL = 'button, a, input, select, textarea, [role="switch"], [role="checkbox"], .glim-info';

// PackageGroup is a plain block inside the list card — not a nested card. Its
// totals are over every link in the package, folded or not: a collapsed package
// that stops counting turns the header into a number that changes when you click
// a chevron.
function PackageGroup({
  name,
  items,
  base,
  ctx,
  columns,
  selection,
  collapsed,
  onToggleCollapsed,
  indexOffset,
  dnd,
  onSelect,
  onSelectTask,
  onOpenProperties,
  onOpenPropertiesTask,
}: {
  name: string;
  items: Task[];
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  selection?: Selection;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  indexOffset: number;
  /** The row drag-to-reorder machinery — see TaskListCard, the one place it is built. */
  dnd: RowDnD;
  /** Click-to-select (TaskListCard's own selectUnit) - see TaskRow's own
   *  identical prop for why the modifier keys travel up rather than being
   *  read here. */
  onSelect?: (e: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }) => void;
  /** The same selection engine, for one child row rather than the whole
   *  package - forwarded to each TaskRow below as its own onSelect. */
  onSelectTask?: (id: string, e: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }) => void;
  /** TaskRow's own onOpenProperties, for a double-click on the package
   *  header itself. */
  onOpenProperties?: () => void;
  /** Forwarded to each child TaskRow as its own onOpenProperties. */
  onOpenPropertiesTask?: (id: string) => void;
}) {
  const allSelected = selection && items.every((x) => selection.ids.has(x.id));
  const dragging = dnd.draggingPackage === name;
  const ytdlpHost = items.find((x) => variantKindOf(x) && x.host)?.host;

  return (
    <section>
      <div
        // The name is the only identity a package has on the wire, and it is
        // legitimately empty for the ungrouped one — so the attribute is present
        // and empty rather than absent, and the page tells the two apart.
        data-package-row={name}
        // Slid out of the way by a drag in flight exactly like a link row - see
        // TaskRow's own identical style above, and previewOffsets for the
        // arithmetic. This is the half jdp was missing (2026-09-03: "die ordner
        // über die man drüber hoovert müssen live verrutschen"): a folder header
        // is a row like any other here, so the folders a drag passes step aside
        // while the pointer is still down.
        style={{ ...ROW_GRID, ...dnd.slide({ kind: 'package', name }) }}
        // Click-to-select (jdp, 2026-08-26 - see TaskRow's own identical
        // comment for the full request): a plain click on the header now
        // selects the whole package instead of folding it - the twisty
        // button beside the name is CONTROL's own match, so it keeps
        // folding/unfolding on its own click exactly as before.
        onClick={(e) => {
          if (e.target instanceof Element && e.target.closest(CONTROL)) return;
          onSelect?.(e);
        }}
        onDoubleClick={(e) => {
          if (e.target instanceof Element && e.target.closest(CONTROL)) return;
          onOpenProperties?.();
        }}
        // Drags the whole package as one block — see TaskRow's own drag
        // handlers above for the identical pattern applied to one link.
        draggable={dnd.enabled}
        onDragStart={(e) => {
          if (!dnd.enabled || (e.target instanceof Element && e.target.closest(CONTROL))) {
            e.preventDefault();
            return;
          }
          dnd.startPackage(name);
          e.dataTransfer.effectAllowed = 'move';
          e.dataTransfer.setData('text/plain', name);
        }}
        onDragEnd={dnd.end}
        onDragOver={(e) => {
          if (!dnd.active) return;
          e.preventDefault();
          dnd.previewOverPackage(name, e);
        }}
        onDrop={(e) => dnd.dropOnPackage(name, e)}
        // A colour step, not a rule: the header sits on the quiet surface and
        // the links inside it sit on the card, which is the whole of the weight
        // difference between a container and its contents. The selected-state
        // background REPLACES the quiet one rather than sitting alongside it -
        // two background-color utilities on one element race in Tailwind's
        // generated stylesheet order (not class-string order), and the quiet
        // one was silently winning, making a selected package invisible.
        className={`grid cursor-pointer select-none items-center ${
          allSelected ? 'bg-accent/20' : 'bg-carbon-surface2/80'
        } px-3 py-2.5 transition-colors hover:bg-carbon-surface2 ${dragging ? 'opacity-50' : ''}`}
      >
        {columns.map((col) => (
          <div
            key={col.id}
            className={`min-w-0 truncate px-2 text-[12px] text-carbon-textSub ${
              col.align === 'end' ? 'text-end' : col.align === 'center' ? 'text-center' : 'text-start'
            } ${col.numeric ? 'glim-num' : ''}`}
          >
            {col.id === 'name' ? (
              <PackageName name={name} items={items} collapsed={collapsed} onToggle={onToggleCollapsed} />
            ) : (
              col.aggregate?.(items, ctx)
            )}
          </div>
        ))}

        {/* The actions gutter - empty for most packages, but the gear badge
            for one whose own "Variante" rows share a host (variantKindOf is
            '' for anything not yt-dlp-routed). One cell per track: a spare
            one wraps the grid onto a second line. */}
        <div className="flex items-center justify-end">
          {ytdlpHost && <HosterPresetButton host={ytdlpHost} base={base} />}
        </div>
      </div>

      {!collapsed && (
        <div className="flex flex-col">
          {items.map((x, i) => (
            <TaskRow
              key={x.id}
              task={x}
              index={indexOffset + i}
              base={base}
              ctx={ctx}
              columns={columns}
              selection={selection}
              dnd={dnd}
              onSelect={onSelectTask ? (e) => onSelectTask(x.id, e) : undefined}
              onOpenProperties={onOpenPropertiesTask ? () => onOpenPropertiesTask(x.id) : undefined}
            />
          ))}
        </div>
      )}
    </section>
  );
}

/**
 * What one row drag is carrying — a single link, or a whole package moved as
 * one block. See TaskListCard's own "Row drag-to-reorder" section, the only
 * place this is built; TaskRow and PackageGroup only read it.
 */
type RowDragKey = { kind: 'task'; id: string } | { kind: 'package'; name: string };

/**
 * The one string a row is known by across the whole drag: the geometry snapshot,
 * the previewed arrangement and the per-row offset all key on this.
 *
 * A package name and a task id live in the same map, so the kind has to be part
 * of the key - a folder called after one of its own links would otherwise share
 * a slot with it. `pkg:` for the unnamed package is a real key, not a missing
 * one, which is the same reason data-package-row is present-and-empty rather
 * than absent.
 */
function rowKey(u: RowDragKey): string {
  return u.kind === 'task' ? `task:${u.id}` : `pkg:${u.name}`;
}

/** The bundle TaskRow and PackageGroup share, built once per render in TaskListCard. */
interface RowDnD {
  /** False in a sorted view — see dndEnabled in TaskListCard for why. */
  enabled: boolean;
  /** Whether some row or package is currently mid-drag, anywhere in the list. */
  active: boolean;
  draggingTask: string | null;
  draggingPackage: string | null;
  startTask: (id: string) => void;
  startPackage: (name: string) => void;
  end: () => void;
  dropOnTask: (id: string, e: DragEvent<HTMLElement>) => void;
  dropOnPackage: (name: string, e: DragEvent<HTMLElement>) => void;
  /** Called from onDragOver, not just onDrop - what makes the rest of the
   *  list move out of the way live instead of only on release. */
  previewOverTask: (id: string, e: DragEvent<HTMLElement>) => void;
  previewOverPackage: (name: string, e: DragEvent<HTMLElement>) => void;
  /** How far this row has to slide to show where the drag in flight would put
   *  it, as the inline style that does it - an empty object when no drag is
   *  running. Every link row and every folder header spreads this into its own
   *  style; see TaskListCard's previewOffsets for where the numbers come from. */
  slide: (unit: RowDragKey) => CSSProperties;
}

/**
 * The header: column labels, and the three things you can do to a column.
 *
 * The label is the draggable element and the resize handle is its sibling rather
 * than a child, because a handle inside the drag source turns every resize into
 * a half-started reorder.
 */
function Header({
  layout,
  sort,
  onSort,
  onReorder,
  onResize,
  onResizeReset,
  onMenu,
}: {
  layout: ResolvedLayout;
  sort: SortState | null;
  onSort: (id: ColumnId) => void;
  onReorder: (id: ColumnId, target: ColumnId, after: boolean) => void;
  onResize: (id: ColumnId, phase: 'start' | 'move' | 'end', e: PointerEvent<HTMLElement>) => void;
  onResizeReset: (id: ColumnId) => void;
  onMenu: (at: { x: number; y: number }) => void;
}) {
  const { t } = useT();
  const [dragId, setDragId] = useState<ColumnId | null>(null);

  function drop(e: DragEvent<HTMLDivElement>, target: ColumnDef) {
    e.preventDefault();
    const from = dragId;
    setDragId(null);
    if (!from || from === target.id) return;
    const r = e.currentTarget.getBoundingClientRect();
    const middle = r.left + r.width / 2;
    const after = document.documentElement.dir === 'rtl' ? e.clientX < middle : e.clientX > middle;
    onReorder(from, target.id, after);
  }

  return (
    <div
      style={ROW_GRID}
      onContextMenu={(e) => {
        e.preventDefault();
        onMenu({ x: e.clientX, y: e.clientY });
      }}
      className="grid items-center border-b border-carbon-border/60 px-3 py-1 select-none"
    >
      {layout.visible.map((col) => {
        const sorted = sort?.id === col.id ? sort.dir : null;
        const sortable = !!col.compare;
        return (
          <div
            key={col.id}
            className={`relative flex items-center ${dragId === col.id ? 'opacity-50' : ''}`}
            onDragOver={(e) => dragId && e.preventDefault()}
            onDrop={(e) => drop(e, col)}
          >
            <button
              type="button"
              draggable
              onDragStart={(e) => {
                setDragId(col.id);
                e.dataTransfer.effectAllowed = 'move';
                // Firefox starts no drag at all without payload on the transfer.
                e.dataTransfer.setData('text/plain', col.id);
              }}
              onDragEnd={() => setDragId(null)}
              onClick={() => sortable && onSort(col.id)}
              aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : undefined}
              className={`flex min-w-0 flex-1 items-center gap-1 px-2 py-1.5 text-[11px] font-semibold uppercase
                tracking-wide transition-colors ${
                  col.align === 'end' ? 'justify-end' : col.align === 'center' ? 'justify-center' : 'justify-start'
                } ${sorted ? 'text-carbon-text' : 'text-carbon-textMuted hover:text-carbon-textSub'}`}
            >
              <span className="truncate">{t(col.labelKey)}</span>
              {sorted === 'asc' && <IconArrowUp width={11} height={11} className="shrink-0" />}
              {sorted === 'desc' && <IconArrowDown width={11} height={11} className="shrink-0" />}
            </button>

            {/* Double-click gives a column its built-in width back, which is the
                only way out of a column dragged down to its minimum. */}
            <span
              role="separator"
              aria-orientation="vertical"
              aria-label={t('columns.resizeHint')}
              title={t('columns.resizeHint')}
              onPointerDown={(e) => onResize(col.id, 'start', e)}
              onPointerMove={(e) => onResize(col.id, 'move', e)}
              onPointerUp={(e) => onResize(col.id, 'end', e)}
              // A drag the browser takes away (a context menu, a window switch)
              // must still settle the width, or the table keeps a hand-painted
              // track list that the next render silently undoes.
              onPointerCancel={(e) => onResize(col.id, 'end', e)}
              onDoubleClick={() => onResizeReset(col.id)}
              className="absolute inset-y-0 end-0 z-10 w-2 cursor-col-resize touch-none"
            />
          </div>
        );
      })}

      {/* One bubble for the whole row, not one per column (SelectionStrip's
          own `<InfoBubble tip={t('remove.keys')} />` used to be the same
          call: a repeated small control explained once rather than on every
          instance of it) — a column header already carries its own visible
          label, so a native `title` repeating "sort by this column" forty
          columns over was the plain-tooltip case this app's own convention
          puts behind a bubble instead. columns.headerHint covers sorting
          too (see its own text), and the per-column title is gone above.
          Lives in the trailing gutter now, not a leading one of its own
          (jdp, 2026-08-26: "Die infobubble in der kopfzeile bitte ganz
          nach rechts verschieben. in der liste fängt jetzt wo die
          checkboxen fehlen alles zu weit rechts an. bitte weiter nach
          links verschieben." - the leading gutter existed only to hold
          this bubble in grid-alignment with GUTTER_LEAD below; removing
          both here and from every row's own alignment placeholder is what
          actually lets the whole table shift left into the space the
          checkbox column used to own, not just moving the bubble alone
          would have). */}
      <div className="flex items-center justify-center">
        <InfoBubble tip={t('columns.headerHint')} />
      </div>
    </div>
  );
}

// --- The properties panel --------------------------------------------------

/** Which box was edited. Nothing else is sent - see TaskProperties. */
type PropField = 'name' | 'dir' | 'comment' | 'priority' | 'autoExtract';

/**
 * The priorities, from the server, low to high so the strip reads as a scale
 * rather than as a list somebody happened to order that way.
 *
 * This used to be five hardcoded steps with a key set of their own, while the
 * right-click menu offered the server's seven. One app, two answers to "how
 * many priorities are there", and the panel's five could not even express the
 * outer two the queue sorts by. The server's list is the list; priorityChoices()
 * fetches it once per session and both callers read the same copy.
 */
function usePriorities(): { id: string; label: TranslationKey }[] {
  const [choices, setChoices] = useState<PriorityChoice[]>([]);
  useEffect(() => {
    let live = true;
    void priorityChoices().then(
      (p) => {
        if (live) setChoices(p);
      },
      () => {
        /* the strip stays empty rather than offering a guess */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  // Reversed: the server sends highest first, because that is the order a menu
  // reads best in, and a slider reads the other way.
  return choices
    .slice()
    .reverse()
    .map((p) => ({ id: String(p.value), label: `priority.${p.id}` as TranslationKey }));
}

/** The tri-state as a tab id. `undefined` is "inherit", never "off". */
const extractId = (t: Task): string =>
  t.autoExtract === undefined ? 'inherit' : t.autoExtract ? 'on' : 'off';

/**
 * agree reads one field off the whole selection and answers with the value they
 * share - or null when they do not, which is a third answer and not an empty
 * one.
 */
function agree<T>(tasks: Task[], pick: (t: Task) => T): T | null {
  if (tasks.length === 0) return null;
  const first = pick(tasks[0]);
  return tasks.every((x) => pick(x) === first) ? first : null;
}

/**
 * TaskProperties edits what is selected - one row or forty, the same panel and
 * the same request.
 *
 * The rule everything here follows: a field is sent ONLY if it was edited. Not
 * "if it differs from what was loaded", and above all not "if it is non-empty".
 * A selection whose rows disagree opens with an empty box, and an empty box that
 * gets sent writes nothing over forty comments, forty folders and forty
 * passwords in one click - a loss nobody would connect to the field they never
 * touched. So the boxes carry a placeholder rather than a value, and `touched`
 * is set by the change handler rather than derived by comparison, which is what
 * keeps "they disagree" and "I cleared this on purpose" apart. Both look like an
 * empty string on the wire; only one of them is in the request at all.
 *
 * `ids` is the whole selection and `tasks` is the part of it this list can see.
 * They are not the same thing - a quick filter can hide a selected row - and the
 * split is deliberate: what is WRITTEN is the whole selection, what is SHOWN is
 * read off the rows that are on screen. A hidden row can only ever make the
 * panel show a value as agreed when it is not, and since nothing is written
 * unless it was edited, that costs a placeholder and never a value.
 */
export function TaskProperties({ ids, tasks, base }: { ids: string[]; tasks: Task[]; base: string }) {
  const { t } = useT();
  const { toast } = useToast();
  const priorities = usePriorities();

  // Read once, at mount. The panel is remounted whenever the selection changes
  // (see the key in TaskListCard), so this is the only moment these values are
  // the ones the user is looking at - reading them again on every render would
  // pull a half-typed box back to what the server last broadcast.
  const [start] = useState(() => ({
    name: tasks.length === 1 ? tasks[0].name : '',
    dir: agree(tasks, (x) => x.dir ?? ''),
    comment: agree(tasks, (x) => x.comment ?? ''),
    priority: agree(tasks, (x) => String(x.priority ?? 0)),
    autoExtract: agree(tasks, extractId),
  }));

  const [name, setName] = useState(start.name);
  const [dir, setDir] = useState(start.dir ?? '');
  const [comment, setComment] = useState(start.comment ?? '');
  const [priority, setPriority] = useState(start.priority);
  const [extract, setExtract] = useState(start.autoExtract);
  const [touched, setTouched] = useState<Set<PropField>>(() => new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  function edit<T>(field: PropField, set: (v: T) => void): (v: T) => void {
    return (v) => {
      set(v);
      setTouched((s) => (s.has(field) ? s : new Set(s).add(field)));
    };
  }

  // The explanation and, where the rows disagree, the sentence that says why the
  // box is empty. One helper, so no field can carry the placeholder without also
  // carrying its reason.
  const hint = (text: string, mixed: boolean) => (mixed ? `${text} ${t('props.mixedHint')}` : text);
  const placeholder = (mixed: boolean) => (mixed ? t('props.mixed') : undefined);

  async function apply(): Promise<void> {
    const opts: TaskOptionsPatch = {};
    if (touched.has('name')) opts.name = name;
    if (touched.has('dir')) opts.dir = dir;
    if (touched.has('comment')) opts.comment = comment;
    if (touched.has('priority') && priority !== null) opts.priority = Number(priority);
    // null, not undefined: "inherit the global switch" is a value the server has
    // to be told, and undefined would be dropped by JSON.stringify along with
    // every field the user left alone.
    if (touched.has('autoExtract') && extract !== null) {
      opts.autoExtract = extract === 'inherit' ? null : extract === 'on';
    }

    setBusy(true);
    setError('');
    const r = await setTaskOptions(ids, opts, base);
    setBusy(false);
    if (!r.ok) {
      // These routes refuse with a sentence, and the sentence is the whole point
      // of refusing: a rename that could not happen has a reason, and hiding it
      // behind "save failed" leaves the row promising a name the folder does not
      // have.
      setError((await r.text()).trim() || t('list.optionsFailed'));
      return;
    }
    setTouched(new Set());
    toast(t('settings.saved'), 'ok');
  }

  return (
    // The right-click belongs to the browser in here. The page above this puts a
    // context menu on the whole list area and calls preventDefault on every
    // reading of it, which inside a text box means no paste entry.
    <section aria-label={t('props.title')} onContextMenu={(e) => e.stopPropagation()}>
      <Card className="flex flex-col gap-4">
        <SectionTitle
          right={
            <span className="glim-num text-xs text-carbon-textMuted">
              {ids.length} {t('select.count')}
            </span>
          }
        >
          {t('props.title')}
        </SectionTitle>

        {/* Only over a single row, and left out rather than greyed out over
            several: a name is an identity, not a property. Forty rows given one
            name is forty downloads pointed at one destination, and the server
            refuses it for the same reason. */}
        {ids.length === 1 && (
          <Field label={t('props.name')} hint={t('props.nameHint')}>
            <TextInput
              value={name}
              spellCheck={false}
              onChange={(e) => edit('name', setName)(e.target.value)}
            />
          </Field>
        )}

        {/* One row, not two stacked full-width fields (jdp, 2026-08-25:
            "Ordner und Kommentar in eine Zeile") - the same
            grid-cols-2 pattern the priority/auto-extract row below already
            uses. Comment is a single-row TextArea now, not rows={2}: it
            shares TextInput's own inputClass (px-3/py-2/text-sm) so a
            rows={1} textarea lands at the same height as Folder's TextInput
            beside it (jdp: "Kommentarfeld gleich hoch wie das eingabefeld
            des Ordners") - still resize-y, so a longer comment can still be
            grown by hand rather than always reserving the space for one. */}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label={t('task.folder')}
            hint={hint(t('settings.downloadDirHint'), start.dir === null)}
          >
            <TextInput
              dir="ltr"
              value={dir}
              spellCheck={false}
              placeholder={placeholder(start.dir === null)}
              onChange={(e) => edit('dir', setDir)(e.target.value)}
            />
          </Field>

          <Field label={t('props.comment')} hint={hint(t('props.commentHint'), start.comment === null)}>
            <TextArea
              rows={1}
              value={comment}
              placeholder={placeholder(start.comment === null)}
              onChange={(e) => edit('comment', setComment)(e.target.value)}
            />
          </Field>
        </div>

        {/* A set of controls, so FieldGroup and not Field: a <label> hands its
            click to the first thing inside it, which here would pick a priority
            every time somebody read the caption.

            Priority and auto-extract read as one decision (jdp: "Priorität und
            Archive entpacken: beide sollen horizontale Selektoren sein und in
            eine Zeile kommen") — the well variant (Tabs.tsx), the same tight
            segmented-control treatment the Look page's own shape and theme
            pickers already use for exactly this complaint, in place of the
            loose default chip row; side by side on one grid row instead of
            each stacked full-width, matching DownloadsSettings.tsx's own
            two-fields-per-row pattern.

            The well track sizes each segment to a fixed width rather than to
            its label (Tabs.tsx: 200px, ported from BombVault's own picker),
            so priority's own seven options run wider than either grid column
            gives it at ordinary widths. overflow-x-auto on the track itself,
            not on the grid or the card, keeps that scroll local to the one
            control instead of ever pushing the panel — or the page — sideways. */}
        <div className="grid gap-4 sm:grid-cols-2">
          <FieldGroup
            label={t('props.priority')}
            hint={hint(t('props.priorityHint'), start.priority === null)}
          >
            <div className="overflow-x-auto">
              <Tabs
                variant="well"
                size="sm"
                className="w-fit"
                label={t('props.priority')}
                active={priority}
                onSelect={edit('priority', setPriority)}
                items={priorities.map((p) => ({ id: p.id, label: t(p.label) }))}
              />
            </div>
          </FieldGroup>

          <FieldGroup
            label={t('props.autoExtract')}
            hint={hint(t('props.autoExtractHint'), start.autoExtract === null)}
          >
            <div className="overflow-x-auto">
              <Tabs
                variant="well"
                size="sm"
                className="w-fit"
                label={t('props.autoExtract')}
                active={extract}
                onSelect={edit('autoExtract', setExtract)}
                items={[
                  { id: 'inherit', label: t('props.inherit') },
                  { id: 'on', label: t('props.on') },
                  { id: 'off', label: t('props.off') },
                ]}
              />
            </div>
          </FieldGroup>
        </div>

        <div className="flex items-center gap-3">
          <Button disabled={touched.size === 0 || busy} onClick={() => void apply()}>
            {t('settings.save')}
          </Button>
          {error && <span className="text-sm text-statusFail">{error}</span>}
        </div>
      </Card>
    </section>
  );
}

// TaskListCard holds every package group on one surface.
export function TaskListCard({
  groups,
  base,
  selection,
  profile = 'downloads',
  title,
  hint,
  hue,
}: {
  groups: [string, Task[]][];
  base: string;
  selection?: Selection;
  /**
   * Which stored layout this list uses. The collector and the downloads list
   * want different columns, and one shared layout would mean switching a column
   * off on the list where it is useless switches it off where it is the point.
   */
  profile?: ListProfile;
  /** This card's own title-badge (jdp, 2026-08-25: the list itself was the
   *  one card left without one) - pre-translated by the caller, the same
   *  way every other shared component here takes its label text rather
   *  than a translation key, since Collector.tsx and Downloads.tsx each
   *  want a name distinct from their own page heading, not this file
   *  guessing which page it is from `profile`. */
  title: string;
  /** What this list IS, for the bubble on the title badge. Optional: the
   *  collector's own card explains itself from AddLinksForm's badge, so only
   *  the downloads list passes one (jdp, 2026-09-05, for the second of the
   *  two first-touch strips). */
  hint?: string;
  /** This card's own rainbow position, independent of whatever hues the
   *  caller's own hero row or badge row already used - see Collector.tsx's
   *  and Downloads.tsx's own call sites for why each picks a different
   *  number. */
  hue?: number;
}) {
  const { t } = useT();
  // Only the row reorder reports through this so far (see dropRow): the queue
  // refuses a reorder with a sentence, and a drag that is refused in silence
  // reads as a drag the app never received.
  const { toast } = useToast();
  // One subscription for the whole table rather than one per row: the palette
  // changes for every row at once anyway.
  useRainbow();

  const [stored, setStored] = useUIState<ColumnLayout | null>(`list.columns.${profile}`, null);
  const [storedSort, setSort] = useUIState<SortState | null>(`list.sort.${profile}`, null);
  const { collapsed, toggle } = useCollapsedPackages(profile);
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);
  // The properties panel's own visibility (jdp, 2026-08-26: "wenn man
  // einmal auf einen link oder einen ordner klickt kommt sofort die
  // eigenschaften card. die soll erst erscheinen bei doppelklick") -
  // decoupled from selection itself now: selecting something (single
  // click, Ctrl-click, Shift-range) no longer opens the panel as a side
  // effect, only a double-click does (TaskRow/PackageGroup's own
  // onOpenProperties). Reset to closed on every NEW selection - selecting
  // something else while the panel is open closes it again, so it always
  // takes a fresh double-click to reopen it for whatever is selected now,
  // the same way it takes a fresh one to open it the first time.
  const [propertiesOpen, setPropertiesOpen] = useState(false);

  const tableRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ id: ColumnId; startX: number; startWidth: number; width: number } | null>(null);

  const layout = useMemo(() => resolveLayout(profile, stored), [profile, stored]);
  const ctx = useMemo<CellContext>(() => ({ t, base }), [t, base]);

  // A sort on a column that is currently hidden is ignored rather than cleared,
  // so showing the column again brings the order back with it. Applying it while
  // its column is invisible would be a list in an order with nothing on screen
  // to explain it.
  const sort = storedSort && !layout.hidden.has(storedSort.id) ? storedSort : null;
  const view = useMemo(() => applySort(groups, sort), [groups, sort]);

  // --- Click-to-select (jdp, 2026-08-26: "in der linkliste soll man
  // links und ordner mit einem klick markieren können, nicht den ordner
  // aufklappen. die checkbox spalte können wir wegmachen. mehrere links
  // oder ordner soll man mit klick und strg oder umschalttaste auswählen
  // können. wie in windows") -------------------------------------------
  //
  // selectableOrder is the flat, on-screen order a Shift-range walks: every
  // package header first, then - only while that package is expanded - its
  // own rows, exactly the order the table below actually renders them in. A
  // collapsed package contributes only itself; Shift-clicking across one
  // selects the whole folded package as a single step, the same as if its
  // rows had never been individually visible to click between.
  const selectableOrder = useMemo(() => {
    const out: { kind: 'task' | 'package'; key: string; ids: string[] }[] = [];
    for (const [name, items] of view) {
      out.push({ kind: 'package', key: name, ids: items.map((x) => x.id) });
      if (!collapsed.has(name)) {
        for (const x of items) out.push({ kind: 'task', key: x.id, ids: [x.id] });
      }
    }
    return out;
  }, [view, collapsed]);

  // The last unit clicked plain or with Ctrl/Cmd - what a Shift-click
  // measures its range from. An index into selectableOrder rather than a
  // remembered key, so a Shift-click still works after the list itself has
  // re-sorted or re-filtered, as long as the anchor unit is still on screen
  // somewhere. Deliberately NOT moved by a Shift-click itself (see
  // selectUnit below) - the same behaviour Explorer's own shift-click has:
  // clicking further away with Shift still held extends or shrinks the
  // SAME range rather than starting a new one from wherever the last
  // Shift-click landed.
  const selectAnchor = useRef<number | null>(null);

  function selectUnit(
    kind: 'task' | 'package',
    key: string,
    ids: string[],
    e: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean },
  ): void {
    if (!selection) return;
    const index = selectableOrder.findIndex((u) => u.kind === kind && u.key === key);
    if (index < 0) return;
    if (e.shiftKey && selectAnchor.current !== null) {
      const lo = Math.min(selectAnchor.current, index);
      const hi = Math.max(selectAnchor.current, index);
      const range = new Set<string>();
      for (let i = lo; i <= hi; i++) selectableOrder[i].ids.forEach((id) => range.add(id));
      selection.set(range);
      return; // The anchor itself does not move - see its own comment above.
    }
    if (e.ctrlKey || e.metaKey) {
      const next = new Set(selection.ids);
      const allIn = ids.every((id) => next.has(id));
      ids.forEach((id) => (allIn ? next.delete(id) : next.add(id)));
      selection.set(next);
      selectAnchor.current = index;
      return;
    }
    selection.set(new Set(ids));
    selectAnchor.current = index;
  }

  // The rows the properties panel shows values from. The ids it WRITES come
  // straight off the selection, which is a larger set whenever a quick filter is
  // hiding one of them - see TaskProperties for why the two are allowed to
  // differ.
  const chosenIds = selection?.ids;
  const chosen = useMemo(
    () => (chosenIds ? view.flatMap(([, items]) => items).filter((x) => chosenIds.has(x.id)) : []),
    [view, chosenIds],
  );
  // Every new selection (selectUnit always hands `set()` a fresh Set, so
  // this fires on a plain click, a Ctrl-click and a Shift-range alike, not
  // only on a change in WHICH ids are in it) closes the panel again -
  // propertiesOpen's own doc comment above has the full reasoning.
  useEffect(() => setPropertiesOpen(false), [chosenIds]);

  // --- Row drag-to-reorder ---------------------------------------------
  //
  // A drag unit is one link or one whole package, moved by the same gesture
  // ("links/ordner", jdp) and built on the same native HTML5 machinery
  // Header's own column reorder already uses above: a "what is being
  // dragged" key, onDragStart/onDragOver/onDrop on every draggable and
  // droppable element, and a rect-vs-pointer check at drop time to decide
  // before or after.
  //
  // Only offered in queue-order view — a client-side sort is documented
  // above (applySort's own doc comment) as a VIEW and never the queue
  // itself, and band-mates a size or status sort has scattered across the
  // table would rarely even land next to each other to drag between. The
  // sortedView banner right above the table already offers the way back.
  const dndEnabled = !sort;
  const [rowDrag, setRowDrag] = useState<RowDragKey | null>(null);
  // The row(s) currently under the pointer mid-drag, and which half of it —
  // updated on every dragover, not just the eventual drop. This is what
  // lets the OTHER rows actually move out of the way live instead of only
  // snapping into their new order once the mouse is released (jdp,
  // 2026-08-25: "die elemente sollen live verrutschen wenn ich zb ein link
  // über einen anderen ziehe").
  const [dragOver, setDragOver] = useState<{ target: RowDragKey; after: boolean } | null>(null);

  // A frozen snapshot of every draggable row's own on-screen position, taken
  // once at the START of a drag — see snapshotSlots() below for why this
  // exists at all: without it, previewOver's "which row, which half" read
  // came from whichever DOM element the browser currently delivers dragover
  // to, and that element itself moves the moment the live preview reorders
  // it, feeding its own output back in as its next input (jdp, 2026-08-25:
  // "jetzt springen die einzelnen elemente... die ganze zeit hin und her").
  // A snapshot the reorder itself never touches breaks that loop.
  //
  // It is now the ONE geometry the whole drag runs on: the list keeps rendering
  // its resting order for as long as the pointer is down and every row is slid
  // to its previewed place by a transform (previewOffsets below), so the
  // document flow the snapshot measured is still the true one at every moment
  // of the drag. The hit test and what the eye sees can no longer drift apart,
  // because the second of them is computed FROM the first.
  //
  // In DOM order, and that matters: previewOffsets stacks rows back up in this
  // order and needs the gap between each pair, not only their own boxes.
  const rowSlotsRef = useRef<{ unit: RowDragKey; top: number; bottom: number }[]>([]);

  function snapshotSlots(): void {
    const root = tableRef.current;
    if (!root) {
      rowSlotsRef.current = [];
      return;
    }
    const slots: { unit: RowDragKey; top: number; bottom: number }[] = [];
    root.querySelectorAll<HTMLElement>('[data-task-id],[data-package-row]').forEach((el) => {
      const r = el.getBoundingClientRect();
      const unit: RowDragKey | undefined =
        el.dataset.taskId !== undefined
          ? { kind: 'task', id: el.dataset.taskId }
          : el.dataset.packageRow !== undefined
            ? { kind: 'package', name: el.dataset.packageRow }
            : undefined;
      if (unit) slots.push({ unit, top: r.top, bottom: r.bottom });
    });
    rowSlotsRef.current = slots;
  }

  // Every task actually on screen, flattened out of the package groups in
  // display order — the same tasks `chosen` reads off `view` above, not the
  // raw `groups` prop, so a drag position always matches what is drawn.
  const flatTasks = useMemo(() => view.flatMap(([, items]) => items), [view]);
  const taskById = useMemo(() => new Map(flatTasks.map((x) => [x.id, x] as const)), [flatTasks]);

  // A "band" mirrors the reorder endpoint's own grouping: same priority AND
  // same forced. Each band's own ids, in the order they are drawn right now,
  // is what POST /api/tasks/reorder is sent. That list is deliberately only
  // the part of the band this screen actually shows and can actually move
  // (see `movable` below): a band spans every task the app holds, across the
  // collector tab and the downloads tab and the settled rows alike, and no
  // single list has ever been able to name all of it.
  const bandOf = (x: Task): string => `${x.priority}:${x.forced ? 1 : 0}`;

  // Which rows the wait queue can actually be told to move, and the reason
  // folder drags looked completely dead (jdp, 2026-09-01: "das drag and drop
  // funktioniert überhaupt nicht. fixe es endlich!" and, for this list
  // specifically, "Ich kann ordner nicht per drag and drop verschieben").
  //
  // A finished or failed task is not in the wait queue at all, so naming one
  // in a reorder refuses the WHOLE request (App.ReorderBand, app_queue.go:
  // "task %s is not in the wait queue") - and this list is meant to show
  // both. Every band built below therefore carries only the rows the server
  // will accept. That is not a workaround for the endpoint: it now takes a
  // SUBSET of a band and reads it as "these tasks, in this order, in the
  // slots they already hold", which is exactly what a drag inside one
  // visible list means. Mobile learned the same thing first, see
  // mobile/src/components/PackageList.tsx's `sortierbar`.
  const movable = (x: Task): boolean => x.status !== 'done' && x.status !== 'error';

  const bandOrder = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const x of flatTasks) {
      if (!movable(x)) continue;
      const key = bandOf(x);
      const arr = m.get(key);
      if (arr) arr.push(x.id);
      else m.set(key, [x.id]);
    }
    return m;
    // movable and bandOf are pure module-level-style helpers over their own
    // argument, so flatTasks is genuinely the only input this varies with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flatTasks]);

  // A package's own band, or null when its links disagree — a mixed package
  // has no one band to move it into, so a drop against it is refused rather
  // than guessing which of its tasks the drag should follow.
  //
  // Judged over the movable links only, so a folder holding one finished file
  // beside three queued ones is still a folder you can drag; before, that one
  // settled row was enough to make the whole folder immovable for no reason
  // the person dragging it could see.
  function packageBand(name: string): string | null {
    const items = (view.find(([n]) => n === name)?.[1] ?? []).filter(movable);
    if (items.length === 0) return null;
    const first = bandOf(items[0]);
    return items.every((x) => bandOf(x) === first) ? first : null;
  }

  function unitBand(u: RowDragKey): string | null {
    if (u.kind === 'task') {
      const t = taskById.get(u.id);
      return t && movable(t) ? bandOf(t) : null;
    }
    return packageBand(u.name);
  }

  function unitIds(u: RowDragKey): string[] {
    if (u.kind === 'task') {
      const t = taskById.get(u.id);
      return t && movable(t) ? [u.id] : [];
    }
    return (view.find(([n]) => n === u.name)?.[1] ?? []).filter(movable).map((x) => x.id);
  }

  // The splice math behind both a live preview and the eventual drop: the
  // band `dragged` would end up in if it landed on `target`'s given half
  // right now, or null for a boundary that refuses the move outright (a
  // different band, dropping a unit on itself or part of itself, a mixed
  // package on either end) — the same three reasons dropRow below always
  // refused, just returning "no" instead of silently doing nothing so a
  // live preview can tell "moved" from "invalid, ignore this dragover"
  // too.
  function reorderedBand(dragged: RowDragKey, target: RowDragKey, after: boolean): string[] | null {
    const band = unitBand(dragged);
    if (!band || band !== unitBand(target)) return null;
    const movedIds = unitIds(dragged);
    const targetIds = unitIds(target);
    if (movedIds.some((id) => targetIds.includes(id))) return null;

    const order = bandOrder.get(band) ?? [];
    const without = order.filter((id) => !movedIds.includes(id));
    // Anchored on the target's own edge — its first id when the moved block
    // lands before it, its last when after — so dropping a package (several
    // ids at once) keeps its own internal order and lands as one
    // contiguous run, exactly where a single link would have landed alone.
    const anchor = after ? targetIds[targetIds.length - 1] : targetIds[0];
    const at = without.indexOf(anchor);
    if (at < 0) return null;
    without.splice(after ? at + 1 : at, 0, ...movedIds);
    return without;
  }

  // The one handler behind every row's and every package header's own
  // onDrop — see dropOnTask/dropOnPackage below, which only add the
  // rect-vs-pointer "before or after" read and then call this.
  function dropRow(target: RowDragKey, after: boolean): void {
    const dragged = rowDrag;
    setRowDrag(null);
    setDragOver(null);
    if (!dragged) return;
    // A different band, the dragged unit dropped on itself, or a mixed
    // package on either end: a normal boundary, not a failure. Reverted
    // visually by the drag simply ending above, no request and no toast —
    // the reorder endpoint's own contract is that this backend does not
    // support a list that crosses a band.
    const without = reorderedBand(dragged, target, after);
    if (!without) return;
    // There is still no local override of the task order to unwind if this
    // fails - the next poll/WS tick is what settles rows back where the
    // server actually put them - but a refusal has to SAY something. This
    // used to be a bare `void reorderTasks(...)`, and reorderTasks throws
    // with the server's own sentence (api.ts, ok()); swallowing that meant a
    // rejected drag was indistinguishable from a drag the app never noticed,
    // which is precisely how it was reported ("funktioniert überhaupt
    // nicht"). One toast turns a silent nothing into a reason.
    reorderTasks(without, base).catch((err) =>
      toast(t('list.failed', { error: err instanceof Error ? err.message : String(err) }), 'fail'),
    );
  }

  // The shared tail of both drop handlers: commit the drag where the live
  // preview has been showing it.
  //
  // dragOver, never the rect of the element the pointer happens to be over.
  // Two independent reasons, and the second one is new:
  //
  //   - previewOver below deliberately aims a FOLDER at other folder HEADERS
  //     only, so reading the drop off a link row would commit it against a
  //     different unit than the one the preview just slid it next to - the drag
  //     would land somewhere nobody aimed at.
  //   - Every row is now displaced by a transform for as long as the pointer is
  //     down (previewOffsets), so getBoundingClientRect no longer answers "which
  //     row is this, and which half of it" the way the pointer sees it - it
  //     answers where the row has SLID to. The frozen snapshot previewOver reads
  //     is the only geometry that still describes the list the person is
  //     dragging over, and dragOver is its answer.
  //
  // The rect is still the fallback for a drop that somehow arrives with no
  // preview behind it at all, which is a drop with nothing better to go on.
  function dropAt(target: RowDragKey, e: DragEvent<HTMLElement>): void {
    e.preventDefault();
    if (dragOver) {
      dropRow(dragOver.target, dragOver.after);
      return;
    }
    const r = e.currentTarget.getBoundingClientRect();
    dropRow(target, e.clientY > r.top + r.height / 2);
  }

  function dropOnTask(id: string, e: DragEvent<HTMLElement>): void {
    // A folder dropped onto one of ANOTHER folder's links means "next to that
    // folder", never "into the middle of it": groupByPackage below re-merges
    // every row of a package at the package's first appearance, so a folder
    // spliced between another's links does not stay there - it silently
    // relocates. Redirecting to the link's own package is the outcome that
    // actually exists. (Only reached by a drop that somehow arrives with no
    // preview behind it - dropAt otherwise commits against dragOver.)
    const target: RowDragKey =
      rowDrag?.kind === 'package' ? { kind: 'package', name: taskById.get(id)?.package ?? '' } : { kind: 'task', id };
    dropAt(target, e);
  }

  function dropOnPackage(name: string, e: DragEvent<HTMLElement>): void {
    dropAt({ kind: 'package', name }, e);
  }

  // dragOver's own hit test, shared by every row's and package header's own
  // onDragOver rather than duplicated. Deliberately NOT `e.currentTarget`'s
  // own rect: once the live preview starts moving rows, the element the
  // browser delivers the NEXT dragover to is itself a consequence of the
  // LAST answer this function gave: under a stationary pointer that sits
  // right on the boundary between two rows, that is a closed loop (this
  // function's own output changes what its next input will be), and the
  // symptom is rows endlessly swapping back and forth rather than settling.
  // Reading against rowSlotsRef's frozen, pre-drag snapshot instead means
  // "which row, which half" is a pure function of the pointer's own Y
  // position and never of whatever this function itself just rendered.
  //
  // Still true now that rows are slid rather than reordered, and for the same
  // reason: the browser hit-tests a transformed element where it is PAINTED, so
  // e.currentTarget is the row the preview has moved under the pointer, not the
  // row that lives at that height in the list. Which element the event arrives
  // on is never read here - only e.clientY is.
  function previewOver(e: DragEvent<HTMLElement>): void {
    if (!rowDrag) return;
    const band = unitBand(rowDrag);
    if (!band) return;
    const y = e.clientY;
    let best: { unit: RowDragKey; top: number; bottom: number } | null = null;
    let bestDist = Infinity;
    for (const slot of rowSlotsRef.current) {
      // A whole folder only ever aims at another folder's header. Its links
      // are not landing slots for it: groupByPackage re-merges a package at
      // its first appearance, so "between two links of folder B" is not a
      // place a folder can come to rest, and offering it as a target meant
      // the folder appeared to land there and then turned up somewhere else.
      // A link dragged on its own still aims at anything, unchanged.
      if (rowDrag.kind === 'package' && slot.unit.kind !== 'package') continue;
      if (unitBand(slot.unit) !== band) continue;
      const dist = y < slot.top ? slot.top - y : y > slot.bottom ? y - slot.bottom : 0;
      if (dist < bestDist) {
        bestDist = dist;
        best = slot;
      }
    }
    if (!best) return;
    const after = y > (best.top + best.bottom) / 2;
    const target = best.unit;
    setDragOver((prev) => {
      if (prev && prev.after === after && sameUnit(prev.target, target)) return prev;
      return { target, after };
    });
  }

  function sameUnit(a: RowDragKey, b: RowDragKey): boolean {
    return a.kind === b.kind && (a.kind === 'task' ? a.id === (b as typeof a).id : a.name === (b as typeof a).name);
  }

  // The band order a live drag would produce right now, band id -> ids -
  // falls back to bandOrder unchanged (and so does every OTHER band the
  // current drag has nothing to do with) whenever there is nothing to
  // preview, so PackageGroup below never has to tell "mid-drag" apart from
  // "at rest" itself.
  const liveBandOrder = useMemo(() => {
    if (!rowDrag || !dragOver) return bandOrder;
    const band = unitBand(rowDrag);
    if (!band) return bandOrder;
    const reordered = reorderedBand(rowDrag, dragOver.target, dragOver.after);
    if (!reordered) return bandOrder;
    const next = new Map(bandOrder);
    next.set(band, reordered);
    return next;
    // reorderedBand and unitBand close over bandOrder/view already in their
    // own dependency chain - bandOrder is the one value this actually
    // varies with, plus the drag's own two pieces of state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rowDrag, dragOver, bandOrder]);

  // The arrangement the drag in flight is promising: the same groups the table
  // is showing, in the order they would be in if the pointer were released now.
  //
  // NOT what gets rendered. It used to be, and that is the gap jdp was looking
  // at (2026-09-03: "#784 funktioniert nicht gut. die ordner über die man
  // drüber hoovert müssen live verrutschen"). Rendering this reordered the real
  // rows, which meant the only way to make the move look like a move was to
  // measure every row after the fact and animate it back (a FLIP effect, now
  // gone) - and that measurement is taken WHILE the previous slide is still
  // running, so from the second folder onwards it read a mid-animation box as
  // the row's resting place, computed a nonsense distance from it, and the
  // folders being hovered over snapped or twitched instead of stepping aside.
  //
  // So this is now a description, and previewOffsets below turns it into one
  // translateY per row. The list itself never reorders while the pointer is
  // down; it slides.
  //
  // This used to re-sort each group's own tasks in place, which moved LINKS
  // and could never move a FOLDER: the list of groups itself kept its order,
  // and since reorderedBand keeps a package's ids contiguous, dragging a
  // folder rearranged nothing at all on screen ("Ich kann ordner nicht per
  // drag and drop verschieben", jdp). The preview is now built the way the
  // real list is - one flat run of tasks handed to groupByPackage - so a
  // folder that moved past another folder genuinely comes out in the new
  // place, and so does the answer the server will send back. That is the
  // point of reusing groupByPackage rather than re-deriving a group order
  // here: the preview cannot promise an arrangement the committed order
  // would not produce, because both come out of the same function.
  //
  // Only the dragged band's own slots are refilled, in liveBandOrder's new
  // order, and every other row keeps the slot it holds: the same rule the
  // server applies to a partial band ("put THESE tasks in THIS order, in the
  // slots they already occupy", App.ReorderBand). That is what keeps a
  // second band on screen, and the finished/failed rows that are in no band
  // at all, from being dragged around by a move they have nothing to do
  // with.
  const liveView = useMemo(() => {
    if (!rowDrag || !dragOver) return view;
    const band = unitBand(rowDrag);
    if (!band) return view;
    const reordered = liveBandOrder.get(band);
    if (!reordered) return view;
    const member = new Set(reordered);
    const flat = view.flatMap(([, items]) => items);
    const byId = new Map(flat.map((x) => [x.id, x] as const));
    const slots: number[] = [];
    flat.forEach((x, i) => {
      if (member.has(x.id)) slots.push(i);
    });
    // One slot per id or the two lists describe different things and there is
    // nothing honest to preview - a mismatch would place one task into two
    // slots, which React would then render as two rows with the same key.
    if (slots.length !== reordered.length) return view;
    const shuffled = [...flat];
    slots.forEach((at, i) => {
      const t = byId.get(reordered[i]);
      if (t) shuffled[at] = t;
    });
    return groupByPackage(shuffled);
    // unitBand reads view and taskById, both of which liveBandOrder already
    // varies with; listing them again would only re-run this on renders that
    // cannot change its result.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, rowDrag, dragOver, liveBandOrder]);

  /**
   * How far every row and every folder header has to slide, right now, to show
   * the arrangement liveView above describes: row key -> pixels, and empty when
   * there is nothing to preview.
   *
   * This is the whole animation, and it is the mobile list's own answer rather
   * than a second invention: DragList keeps its rows exactly where they are and
   * gives the ones the drag has passed a translate offset (`versatz`), which is
   * what makes them "sich sofort verschieben wenn man drüberhovert" (jdp,
   * 2026-08-31, about that list). The web list can do the same thing with more
   * precision, because rows here are not one uniform height: a folder header, a
   * folded folder and a link are three different boxes, so the offsets are
   * computed from the real measured ones instead of from a single row height.
   *
   * The arithmetic, all of it off the pre-drag snapshot:
   *
   *   - `wanted` is the previewed arrangement flattened to the rows that are
   *     actually ON SCREEN - a folded folder contributes its header and none of
   *     its links, exactly like the table's own render below.
   *   - Those rows are stacked back up from the first slot's top, each taking
   *     its own measured height, and each keeping the GAP that belongs to its
   *     new position rather than to itself. The gap is a property of the seam
   *     between two rows (the divider between two folder sections), not of the
   *     row that happens to sit above it.
   *   - The offset is then simply "where this row would be" minus "where it
   *     is", and that is a number a CSS transition can animate on its own.
   *
   * Bails out whole rather than in part. A poll that adds or removes a task
   * mid-drag leaves the snapshot describing a list that no longer exists, and
   * half-correct offsets would leave rows lying on top of each other; no
   * preview at all is the honest state, and the drop itself still commits
   * against dragOver, which never depended on this.
   */
  function previewOffsets(): Map<string, number> {
    if (!rowDrag || !dragOver) return NO_OFFSETS;
    const slots = rowSlotsRef.current;
    if (slots.length === 0) return NO_OFFSETS;
    const wanted: string[] = [];
    for (const [name, items] of liveView) {
      wanted.push(rowKey({ kind: 'package', name }));
      if (!collapsed.has(name)) for (const x of items) wanted.push(rowKey({ kind: 'task', id: x.id }));
    }
    if (wanted.length !== slots.length) return NO_OFFSETS;
    const geom = new Map(slots.map((s) => [rowKey(s.unit), s] as const));
    const out = new Map<string, number>();
    let y = slots[0].top;
    for (let i = 0; i < wanted.length; i++) {
      const g = geom.get(wanted[i]);
      if (!g) return NO_OFFSETS;
      out.set(wanted[i], y - g.top);
      const nextSlot = slots[i + 1];
      y += g.bottom - g.top + (nextSlot ? nextSlot.top - slots[i].bottom : 0);
    }
    return out;
  }

  const rowOffsets = previewOffsets();

  const dnd: RowDnD = {
    enabled: dndEnabled,
    active: rowDrag !== null,
    draggingTask: rowDrag?.kind === 'task' ? rowDrag.id : null,
    draggingPackage: rowDrag?.kind === 'package' ? rowDrag.name : null,
    startTask: (id) => {
      // Taken from the DOM at this exact moment, before any reorder preview
      // has ever run for this drag — the one point at which the rendered
      // order is guaranteed to still match the server's own bandOrder.
      snapshotSlots();
      setRowDrag({ kind: 'task', id });
    },
    startPackage: (name) => {
      snapshotSlots();
      setRowDrag({ kind: 'package', name });
    },
    end: () => {
      setRowDrag(null);
      setDragOver(null);
    },
    dropOnTask,
    dropOnPackage,
    previewOverTask: (_id, e) => previewOver(e),
    previewOverPackage: (_name, e) => previewOver(e),
    // Every row in the table gets one of these, including the ones that are not
    // moving: the transition has to already be on a row before its offset
    // changes, or the first step aside it makes is a jump. A row with nothing to
    // do simply carries translateY(0).
    //
    // The whole style DISAPPEARS the moment the drag ends, which is what puts
    // the list back in one frame with no animation - deliberately, and for the
    // same reason as before: what lands after a drop is the server's own order,
    // and sliding into it would read as the app moving something on its own
    // rather than as the answer to what was just dropped.
    //
    // 180ms is the same slide the old FLIP effect used, kept because jdp had
    // already judged that speed ("das zur seite rutschen soll sehr smooth sein",
    // 2026-08-25); only the machinery underneath it is different.
    //
    // No will-change here on purpose. It is the usual reflex next to a transform
    // and it would be wrong at this scale: this runs on every row of a list that
    // can be several hundred long, and a promise of "this will move" on all of
    // them at once buys layers for rows that never move. A transform transition
    // is composited while it runs without being told in advance.
    slide: (unit) => {
      const dy = rowOffsets.get(rowKey(unit));
      if (dy === undefined) return NO_SLIDE;
      return { transform: `translateY(${dy}px)`, transition: 'transform 180ms ease' };
    },
  };

  const template = gridTemplate(layout.visible, layout.widthOf);

  function persist(next: Partial<ColumnLayout>): void {
    setStored({ ...toStored(layout), ...next });
  }

  function toggleColumn(id: ColumnId): void {
    const def = COLUMN_BY_ID.get(id);
    if (!def?.hideable) return;
    const hidden = new Set(layout.hidden);
    if (hidden.has(id)) hidden.delete(id);
    else {
      // Hiding the last one leaves a table with nothing in it, and the menu that
      // would put a column back is anchored to the header that no longer exists.
      if (layout.visible.length <= 1) return;
      hidden.add(id);
    }
    persist({ hidden: [...hidden] });
  }

  function onResize(id: ColumnId, phase: 'start' | 'move' | 'end', e: PointerEvent<HTMLElement>): void {
    if (phase === 'start') {
      e.preventDefault();
      e.currentTarget.setPointerCapture(e.pointerId);
      const w = layout.widthOf(id);
      drag.current = { id, startX: e.clientX, startWidth: w, width: w };
      return;
    }
    const d = drag.current;
    if (!d || d.id !== id) return;
    const rtl = document.documentElement.dir === 'rtl' ? -1 : 1;
    const min = COLUMN_BY_ID.get(id)?.minWidth ?? 40;
    d.width = Math.max(min, Math.round(d.startWidth + (e.clientX - d.startX) * rtl));
    if (phase === 'move') {
      // Painted straight onto the table, off React's render path: re-rendering
      // several hundred rows per pointermove is what makes a column drag stutter,
      // and every row wants the same widths anyway.
      tableRef.current?.style.setProperty(
        '--kl-cols',
        gridTemplate(layout.visible, (x) => (x === d.id ? d.width : layout.widthOf(x))),
      );
      return;
    }
    drag.current = null;
    persist({ widths: { ...layout.widths, [id]: d.width } });
  }

  function resetWidth(id: ColumnId): void {
    const widths = { ...layout.widths };
    delete widths[id];
    persist({ widths });
  }

  let index = 0;

  return (
    // Two surfaces side by side and never one inside the other: the panel is a
    // card of its own under the list, because a card inside a card is the one
    // elevation rule this language does not bend.
    //
    // flex-1 on both, not h-full: harmless where a caller (Downloads.tsx)
    // renders this in normal document flow - a flex item with no flex
    // container ancestor of its own just falls back to its natural content
    // size, so nothing changes there. Meaningful where a caller wraps this
    // in its own flex column (Collector.tsx's own overflow-y-auto wrapper,
    // jdp 2026-08-24: "Das hauptlinkfenster soll immer ... bis ganz nach
    // unten im fenster gehen. egal wie viele links drinn sind") - flex-1
    // rather than a percentage height on purpose: h-full measured correctly
    // on paper here but did not actually resolve in the browser, a known
    // quirk where a percentage height does not reliably propagate through a
    // block box whose own height came from flex-grow plus overflow-auto
    // (confirmed live rather than assumed). flex-grow has none of that
    // ambiguity, so this is the one place in this component that composes
    // with whatever a caller does about height.
    <div className="flex flex-1 flex-col gap-6">
      {/* No overflow-hidden on this outer box (jdp, 2026-08-25: "cardtitelbadge
          des linkhauptfenster und downloadhauptfenster sind nur halb sichtbar
          und abgeschnitten") - the SAME bug and the SAME fix as
          AddLinksForm.tsx's own card (see its doc comment): SectionTitle's own
          pill sits `absolute -top-[11px]`, half over this card's own top edge
          by design, and overflow-hidden on the element that hosts its
          `position: relative` clipped exactly that half off. Unlike
          AddLinksForm.tsx, this card DOES have flush-edged content below the
          title (the table can run edge-to-edge under overflow-x-auto, and the
          rows are not their own rounded shape) - so the clip still needs to
          exist, just scoped to a wrapper that starts BELOW the title instead
          of on the card's own positioning box. rounded-b (not rounded-t) to
          match: the title sits far enough below the card's own top corners
          already (the badge's negative offset only reaches into the pt-4
          above it, never past the card's own top edge), and the h-10 spacer
          after the rows already keeps the last row's own square corners clear
          of the card's rounded bottom corners - but a table that is
          user-scrolled all the way down, or a card too short to show that
          full spacer, still deserves the same rounded-bottom guarantee an
          unclipped square row would otherwise be able to break. */}
      {/* A hand-rolled card rather than <Card>, so the palette position is set
          here by hand - and on the card, not on its title, so everything the
          card holds follows it. */}
      {/* `hue` is optional here, and a card without one must NOT take the
          class: `.glim-hue` with no --item-hue resolves --accent to nothing
          and the badge inside disappears. */}
      <div
        className={`glim-card ${hue !== undefined ? 'glim-hue ' : ''}flex-1`}
        style={hue !== undefined ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
      >
        <div className="px-4 pt-4">
          <SectionTitle hint={hint}>{title}</SectionTitle>
        </div>
        <div className="overflow-hidden rounded-b-[var(--radius-card)]">
          {/* Sorting is a view of the queue and not the queue. Saying so where the
              order is visibly different is the whole of it — a list that quietly
              shows one order while running another is read as a bug in the queue. */}
          {sort && (
            <div className="flex items-center gap-1 px-4 py-2 text-[11px] text-carbon-textMuted">
              <span>{t('list.sortedView')}</span>
              <InfoBubble tip={t('list.sortedViewTip')} />
              <span className="flex-1" />
              <Button kind="ghost" className="px-2 py-1 text-[11px]" onClick={() => setSort(null)}>
                {t('list.queueOrder')}
              </Button>
            </div>
          )}

          {/* min-w-min, not min-w-max: max-content pins the table at the sum of its
              own columns, which overrides the flexible name track entirely and makes
              the list open scrolled off its right edge in any window narrower than
              that sum. With min-content the name column gives way down to its own
              minimum first, and only then does the table start scrolling — which is
              the point at which scrolling is actually the right answer. */}
          <div className="overflow-x-auto">
            <div ref={tableRef} className="min-w-min" style={{ ['--kl-cols' as string]: template } as CSSProperties}>
              <Header
                layout={layout}
                sort={sort}
                onSort={(id) => setSort(nextSort(storedSort, id))}
                onReorder={(id, target, after) =>
                  persist({ order: moveColumn(layout.order.map((c) => c.id), id, target, after) })
                }
                onResize={onResize}
                onResizeReset={resetWidth}
                onMenu={setMenuAt}
              />

              {/* `view`, not liveView, and that is the point: the rows stay in
                  the order the server last gave for the whole of a drag, and
                  each one is SLID to where the drag would put it (previewOffsets
                  above, applied by dnd.slide). Reordering them here instead is
                  what the preview used to do, and it left the move with nothing
                  to animate but an after-the-fact measurement. It also kept the
                  rainbow hues walking a moving list, so colours shuffled under
                  the pointer as a side effect of a reorder nobody had committed
                  yet - `index` below counts the resting order now. */}
              <div className="divide-y divide-carbon-border/60">
                {view.map(([name, items]) => {
                  const folded = collapsed.has(name);
                  const offset = index;
                  if (!folded) index += items.length;
                  return (
                    <PackageGroup
                      key={name || '__none'}
                      name={name}
                      items={items}
                      base={base}
                      ctx={ctx}
                      columns={layout.visible}
                      selection={selection}
                      collapsed={folded}
                      onToggleCollapsed={() => toggle(name)}
                      indexOffset={offset}
                      dnd={dnd}
                      onSelect={(e) => selectUnit('package', name, items.map((x) => x.id), e)}
                      onSelectTask={(id, e) => selectUnit('task', id, [id], e)}
                      onOpenProperties={() => setPropertiesOpen(true)}
                      onOpenPropertiesTask={() => setPropertiesOpen(true)}
                    />
                  );
                })}
              </div>

              {/* The empty space under the rows, and it earns its keep twice: a
                  table that ends flush against the edge of its card reads as cut
                  off, and a right-click needs somewhere to land that is not a row.
                  That is where the list's own menu lives — select all, fold the lot,
                  clean up — the same place a desktop list keeps it. */}
              <div className="h-10" />
            </div>
          </div>
        </div>

        {menuAt && (
          <ColumnMenu
            at={menuAt}
            columns={layout.order}
            hidden={layout.hidden}
            onToggle={toggleColumn}
            onReset={() => {
              setStored(null);
              setMenuAt(null);
            }}
            onClose={() => setMenuAt(null)}
          />
        )}
      </div>

      {/* Keyed on the selection, so picking different rows re-reads the boxes
          instead of carrying one selection's half-typed comment onto another.
          Nothing to edit means no panel: an empty properties panel is a card of
          disabled controls, which is a page that reads as broken. propertiesOpen
          gates it on top of that (jdp, 2026-08-26 - see that state's own doc
          comment): selecting something is no longer enough on its own, a
          double-click is what actually opens it. */}
      {propertiesOpen && chosenIds && chosen.length > 0 && (
        <TaskProperties key={[...chosenIds].join(',')} ids={[...chosenIds]} tasks={chosen} base={base} />
      )}
    </div>
  );
}

// groupByPackage groups tasks by their package, preserving insertion order.
export function groupByPackage(list: Task[]): [string, Task[]][] {
  const m = new Map<string, Task[]>();
  for (const t of list) {
    const arr = m.get(t.package || '');
    if (arr) arr.push(t);
    else m.set(t.package || '', [t]);
  }
  return [...m.entries()];
}
