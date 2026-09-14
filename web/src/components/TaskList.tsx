import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type DragEvent,
  type KeyboardEvent,
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
import {
  pause,
  resume,
  remove,
  startTasks,
  restartTasks,
  recheckTasks,
  setPackage,
  setTaskOptions,
  reorderTasks,
  // Aliased: TaskProperties further down owns local useState setters called
  // setPriority/setForced, and a module-scope import of the same two names
  // would read as those inside it.
  setPriority as setTaskPriority,
  setForced as setTaskForced,
} from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useUIState } from '../lib/uistate';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  ErrorCard,
  IconBadge,
  InfoBubble,
  LoadingCard,
  Modal,
  SectionTitle,
  TextArea,
  TextInput,
  useTooltip,
} from './ui';
import { Tabs } from './Tabs';
import { ColumnMenu } from './ColumnMenu';
import {
  COLUMN_BY_ID,
  Checkbox,
  FOLDER_GLYPH,
  Tip,
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
import { RetrySkipBadge } from './RetryCountdown';
import { TaskDetailPanel } from './taskdetail/TaskDetailPanel';
import { useListKeyboard } from './listKeyboard';
import { rowKey, useRowWindow, type ListRow, type RowDragKey } from './listRows';
import {
  aimAt,
  pastThreshold,
  previewOrder,
  sameUnit,
  selectedBlock,
  stackOffsets,
  type BlockRow,
  type RowSlot,
} from './rowDrag';
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
 * A selected row's own painted ground, as one opaque colour.
 *
 * The same colour `.glim-row-selected` paints (index.css: the accent at 22% over
 * whatever the row lies on, which here is the card's --carbon-surface), written
 * as a mix rather than as an alpha layer so the row's action strip can REPEAT it
 * instead of stacking a second 22% on top of it. Inline on the row, because a
 * selected row keeps this fill under the pointer as well and an inline value is
 * the only one that beats the hover variant beside it.
 */
const SELECTED_GROUND = 'color-mix(in srgb, var(--accent-fixed) 22%, var(--carbon-surface))';

/**
 * The rainbow wash a link row is painting right now, as the class that repeats
 * it on that row's action strip.
 *
 * The wash is an inset box-shadow on the ROW (index.css's .glim-tint rules), and
 * an inset shadow paints under its element's own children - so a strip with a
 * ground of its own covers it and reads as a grey tile punched into a coloured
 * row. Measured in Rainbow mode, dark theme: the row is rgb(79,70,42) and the
 * strip was rgb(57,57,57).
 *
 * Chosen here rather than written as four stacked CSS variants, because a row
 * that is both running and selected would then come down to the order Tailwind
 * happened to generate the two rules in. One row, one class, no ties.
 *
 * The literals are spelled out in full: Tailwind reads the source text, so a
 * class assembled from pieces at runtime is a class that never gets generated.
 */
function rowWash(selected: boolean, active: boolean): string {
  // A selected row's tint replaces its hue wash in every rainbow mode, at rest
  // and under the pointer alike (index.css, .glim-tint.glim-row-selected).
  if (selected) {
    return '[[data-rainbow]_&]:shadow-[inset_0_0_0_999px_color-mix(in_oklab,var(--accent-fixed)_22%,transparent)]';
  }
  // A running row carries the stronger soft tint, in both modes, without hover.
  if (active) return '[[data-rainbow]_&]:shadow-[inset_0_0_0_999px_var(--item-hue-soft)]';
  // And the plain case: on hover, in BOTH modes. It used to split them - at rest
  // under Rainbow, on hover under Reactive - and index.css no longer does, on
  // jdp's call ("die link zeilen sollen im regenbogen modus nicht farbig
  // eingefärbt sein. nur bei mouse over sollen sie farbig werden"). One rule for
  // both is now the honest mirror of the row, and the split version would have
  // painted this strip at rest in Rainbow mode while the row beneath it stayed
  // plain. Invisible today, because the strip only fades in on the same hover -
  // which is exactly the kind of agreement that quietly stops being true.
  return '[[data-rainbow]_&]:group-hover:shadow-[inset_0_0_0_999px_var(--item-hue-wash)]';
}

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
  onOpenProperties,
  current,
  onKeyDown,
  level,
  posinset,
  setsize,
}: {
  task: Task;
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  selection?: Selection;
  /** Position in the rendered list — the rainbow palette position. */
  index: number;
  /** Press, sweep-select and move-by-drag — see TaskListCard, the one place it
   *  is built, and the one place that knows what a press means. */
  dnd: RowDnD;
  /** Opens the properties panel for whatever a single click just selected
   *  (jdp, 2026-08-26: "wenn man einmal auf einen link oder einen ordner
   *  klickt kommt sofort die eigenschaften card. die soll erst erscheinen
   *  bei doppelklick" - a plain click used to open it immediately as a
   *  side effect of selecting anything at all, which made a quick
   *  multi-select impossible without the panel flashing open and shut on
   *  every intermediate click). The double-click's own leading press has
   *  already marked this row by the time this fires - no modifier keys to
   *  read here, only "show it now". */
  onOpenProperties?: () => void;
  /** True for the one row that owns the list's tab stop. Everything focusable
   *  inside a row reads it too, or Tab would still walk four hundred badges. */
  current: boolean;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
  /** The tree's own sibling-set numbers, off the model and never off the
   *  window - see ListRow. */
  level: number;
  posinset: number;
  setsize: number;
}) {
  const { t } = useT();
  const collected = task.status === 'collected';
  const settled = task.status === 'done' || task.status === 'error';
  const unit: RowDragKey = { kind: 'task', id: task.id };
  const dragging = dnd.moving(unit);

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
      // What the window measures. Separate from data-task-id above because a
      // folder header carries the same pair and has no task id to be found by -
      // see useRowWindow's own measureRows.
      data-row-key={rowKey({ kind: 'task', id: task.id })}
      data-row-kind="task"
      role="treeitem"
      // Roving tabindex: one row is the tab stop, not five thousand.
      tabIndex={current ? 0 : -1}
      aria-selected={selection ? selection.ids.has(task.id) : undefined}
      aria-level={level}
      aria-posinset={posinset}
      aria-setsize={setsize}
      onKeyDown={onKeyDown}
      // The live drag preview moves this row by SLIDING it (a transform), not by
      // rendering it somewhere else in the list - see TaskListCard's own
      // previewOffsets. While a drag is in flight this carries a translateY and a
      // transform transition; with no drag the two properties are simply absent
      // again and the row sits where the document flow puts it.
      //
      // --row-ground is the row's own painted ground, and the action strip at the
      // trailing edge wears it - see SELECTED_GROUND and the strip itself. Set
      // here as an inline property for a selected row on purpose: a selected row
      // keeps its accent fill under the pointer too (.glim-row-selected:hover
      // beats the hover utility), and only an inline value beats the hover
      // variant that would otherwise swap the ground out from under the strip.
      style={
        {
          ...hueVars(rainbowAt(index)),
          ...ROW_GRID,
          ...(selection?.ids.has(task.id) ? { '--row-ground': SELECTED_GROUND } : null),
          ...dnd.slide(unit),
        } as CSSProperties
      }
      // ONE PRESS, AND THE ROW DOES NOT KNOW WHAT IT MEANS. Selecting, sweeping
      // a selection over several rows and moving the selection are all the same
      // pointerdown, told apart by what the pointer does next - see
      // TaskListCard's own gesture section, which owns every bit of that.
      //
      // There is no `draggable` here any more, and its absence is the whole
      // point rather than a tidy-up: a native HTML5 drag takes the pointer over
      // as soon as it starts, so a press that might still turn out to be a
      // selection sweep cannot be one while the row is draggable.
      onPointerDown={(e) => dnd.press(e, unit)}
      // Still a double-click and not a second press-and-hold: the properties
      // panel is opened by a gesture that cannot be confused with a move,
      // because a double-click never travels the five pixels that start one.
      onDoubleClick={(e) => {
        if (e.target instanceof Element && e.target.closest(CONTROL)) return;
        onOpenProperties?.();
      }}
      // select-none, unconditionally, and it matters MORE now than it did under
      // the native drag: a press-and-drag that starts over the row's own text
      // (the name or URL column - the columns a hand naturally lands on) draws
      // a blue text selection across half the table while the row sweep is
      // running underneath it. The gesture itself still works, because pointer
      // events do not care what the browser thinks it is selecting, but what
      // you see is two selections at once. The package header beside this row
      // has this for the same reason.
      // bg-accent/20, not the softer bg-accentSoft token this used at first
      // (jdp, 2026-08-26: "wenn eine zeile ausgewählt ist erkennt man das
      // nicht" - accentSoft is 14% alpha, chosen for a hover/drag hint that
      // is meant to stay quiet, and a selected row wants the opposite: a
      // mark somebody actually notices). A real background-color layered
      // over glim-tint's own inset box-shadow rainbow wash rather than
      // fighting it for the same CSS property, so both show at once.
      // THE ROW PAINTS --row-ground AND SO DOES ITS ACTION STRIP: one expression,
      // one place, because this is the third round on the same fault. The strip
      // used to name a surface token of its own, and a token is a guess about
      // what the row is painting - this row hovers to `--carbon-hover` at half
      // alpha over the card, which is not `--carbon-surface2` and not
      // `--carbon-surface` either. Measured on the running instance, dark theme:
      // the hovered row is rgb(45,45,45) and the strip that was meant to match it
      // was rgb(57,57,57); in the light theme the row is rgb(239,239,239) and the
      // strip rgb(232,232,232) - a plate that is too light in one theme and too
      // dark in the other, which is exactly what was reported ("der löschen
      // button hat immer noch den dunklen Hintergrund bzw. rand"). A variable
      // cannot be off by a step: whatever this row paints, the strip paints.
      //
      // The hover mix is the same colour the `bg-carbon-hover/50` utility used to
      // composite to, written as an opaque mix instead of an alpha layer so that
      // the strip can repeat it rather than stack a second layer of it.
      // has-[:focus-visible] is the keyboard's own hover: the strip opens on the
      // row a badge inside it is focused on, and the ground has to arrive with it.
      className={`glim-hue glim-tint select-none ${task.status === 'running' ? 'glim-active' : ''} ${dragging ? 'opacity-50' : ''} ${
        selection?.ids.has(task.id) ? 'glim-row-selected' : ''
      } group relative grid items-center px-3 py-2 transition-colors
        bg-[var(--row-ground)] [--row-ground:transparent]
        [--row-hover:color-mix(in_srgb,var(--carbon-hover)_50%,var(--carbon-surface))]
        hover:[--row-ground:var(--row-hover)] has-[:focus-visible]:[--row-ground:var(--row-hover)]`}
    >
      {columns.map((col) => {
        const node = col.render(task, ctx);
        return (
          <div
            key={col.id}
            dir={col.ltr ? 'ltr' : undefined}
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
            // text-xs is the scale's dense row, which is what a table cell
            // takes. It was 12.5px, a step the four-row table does not have.
            className={`min-w-0 truncate text-xs text-carbon-textSub ${
              col.id === 'name' ? 'pe-2' : 'px-2'
            } ${col.align === 'end' ? 'text-end' : col.align === 'center' ? 'text-center' : 'text-start'} ${col.numeric ? 'glim-num' : ''}`}
          >
            {/* A column that renders plain text carries that text in the house
                bubble, so a name too long for its width is still readable
                without widening the column first. The house bubble, never a
                native `title=` and the operating system's own balloon beside
                it - see columns.tsx's `Tip`. */}
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

      {/* The whole strip floats over the row's trailing edge on hover now, and
          owns no grid track at all (jdp, 2026-09-06: "im downloadtab ist rechts
          eine spalte die leer ist" - a fixed 152px gutter, empty on nearly every
          row, was exactly that column). It carries the row's own ground so the
          cells it covers do not read through it, and the context menu on the
          same row offers every one of these verbs for anybody not using a
          pointer.
          THE GROUND IS THE ROW'S OWN --row-ground, and getting there took three
          rounds. It carried --carbon-surface with an elevation shadow first, then
          --carbon-surface2 on the reasoning that the row hovers to surface2. The
          row does not: it hovers to --carbon-hover at half alpha over the card.
          Measured, dark theme: row rgb(45,45,45) against a strip of rgb(57,57,57);
          light theme: row rgb(239,239,239) against rgb(232,232,232). Both rounds
          were the same mistake - a second opinion about what the row is painting
          - and the report came back both times ("der löschen button hat immer
          noch den dunklen Hintergrund bzw. rand"). There is no opinion left here:
          the row publishes what it paints and the strip reads it.
          No ground of its own means no shadow either: a shadow is what makes a
          surface float, and this one is not floating, it is the row.
          The rainbow wash is the other half of what the row paints, and an inset
          shadow does not reach a child, so the strip repeats the one its row has
          (index.css's own .glim-tint rules). Which one that is depends on the row
          and not on the mode alone, so it is chosen here rather than layered in
          CSS, where an active-and-selected row would come down to stylesheet
          order. In Reactive mode the wash is a hover reveal, so the strip's copy
          is too - except on a selected or running row, which carries its tint in
          that mode at rest.
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
      <div
        // :has() and not :focus-within, which matches the element ITSELF: with
        // focus-within the strip popped open on the focused row and covered its
        // own size, speed and status cells, so a keyboard user walking the list
        // had them permanently hidden behind it on whichever row they stood on.
        // :has() never matches the element itself, so the strip still appears
        // when a badge inside it takes focus and stays out of the way when the
        // row does.
        className={`absolute inset-y-px end-2 z-10 flex items-center gap-1 rounded-[var(--radius-control)]
          bg-[var(--row-ground)] px-1 opacity-0 transition-opacity
          group-hover:opacity-100 [&:has(:focus-visible)]:opacity-100 ${rowWash(
            selection?.ids.has(task.id) ?? false,
            task.status === 'running',
          )}`}
      >
        {collected && (
          <IconBadge
            quiet
            hue={0}
            // Roving tabindex reaches inside the row too: without it Tab walks
            // every badge of every drawn row and the one-stop list is decorative.
            tabIndex={current ? 0 : -1}
            icon={<IconPlay width={16} height={16} />}
            title={t('task.start')}
            aria-label={t('task.start')}
            onClick={() => startTasks([task.id], base)}
          />
        )}
        {task.status === 'running' && (
          <IconBadge
            quiet
            hue={0}
            tabIndex={current ? 0 : -1}
            icon={<IconPause width={16} height={16} />}
            title={t('task.pause')}
            aria-label={t('task.pause')}
            onClick={() => pause(task.id, base)}
          />
        )}
        {task.status === 'paused' && (
          <IconBadge
            quiet
            hue={0}
            tabIndex={current ? 0 : -1}
            icon={<IconPlay width={16} height={16} />}
            title={t('task.resume')}
            aria-label={t('task.resume')}
            onClick={() => resume(task.id, base)}
          />
        )}
        {/* No second opacity layer inside the strip any more: the whole strip
            already appears on hover, so this was a fade inside a fade. */}
        <div className="flex items-center gap-1">
          {collected && (
            <IconBadge
              quiet
              hue={1}
              tabIndex={current ? 0 : -1}
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
          {/* Before Restart and never instead of it. This one appears only
              while a wait is actually running, and it spends the retry that was
              already scheduled rather than granting a new one. Restart beside it
              stays the plain "run this again" for a row that is finished or done
              waiting. No guard needed at the call site: RetrySkipBadge draws
              nothing unless a retry is genuinely pending, which is narrower than
              `settled` and keeps the badge off every finished download. */}
          <RetrySkipBadge task={task} base={base} focusable={current} />
          {settled && (
            <IconBadge
              quiet
              hue={3}
              tabIndex={current ? 0 : -1}
              icon={<IconRetry width={16} height={16} />}
              title={t('task.restart')}
              aria-label={t('task.restart')}
              onClick={() => restartTasks([task.id], base)}
            />
          )}
          <IconBadge
            quiet
            hue={4}
            tabIndex={current ? 0 : -1}
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
  focusable,
}: {
  name: string;
  items: Task[];
  collapsed: boolean;
  onToggle: () => void;
  /** Whether this row currently owns the list's tab stop - see TaskRow's own
   *  `current`. The twisty is a real button and would otherwise be its own
   *  tab stop on every drawn package. */
  focusable: boolean;
}) {
  const { t } = useT();
  const priorityNames = usePriorityNames();
  const done = items.filter((x) => x.status === 'done').length;
  const label = t(collapsed ? 'task.expand' : 'task.collapse');
  // The twisty draws a glyph and nothing else, so it needs a tooltip
  // unconditionally - and it takes the house bubble, never the OS balloon the
  // native attribute draws (see columns.tsx's `Tip`).
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;

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
        tabIndex={focusable ? 0 : -1}
        aria-expanded={!collapsed}
        aria-label={label}
        {...hover}
        className="grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-control)] text-carbon-textSub
          transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
      >
        <Twisty open={!collapsed} />
      </button>
      {tip.node}
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
      {/* text-sm is the scale's body row; it was 13.5px, which is not one of
          its four steps. */}
      <Tip
        tip={`${name || t('task.ungrouped')} - ${count}`}
        className="min-w-[5rem] flex-1 truncate text-sm font-semibold text-carbon-text"
      >
        {name || t('task.ungrouped')}
      </Tip>
      {/* The count hangs in the name's own bubble as well, so a column too
          narrow to show it has not hidden anything that cannot be got at. Its
          own online ratio used to print here too ("5/5 online") - removed (jdp,
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
function HosterPresetButton({ host, base, focusable }: { host: string; base: string; focusable: boolean }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const label = `${t('collector.hosterPreset')} · ${host}`;
  return (
    <>
      <IconBadge
        hue={0}
        tabIndex={focusable ? 0 : -1}
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

// The two failures here are two different kinds and are reported two different
// ways, which is the judgment GlimStone's failure-feedback section asks for:
// does a fresh click of the SAME button replace this exact message?
//
//   the SAVE failed  - yes. A clicked control that could not do what it was
//                      asked reports through the two channels every action
//                      uses: the sentence goes to a toast, and the button that
//                      was pressed shakes. It used to leave the raw server
//                      string sitting in the footer until the next click
//                      overwrote it, which is the one shape that section rules
//                      out by name - it never clears itself, so an hour-old
//                      failure looks exactly as current as a fresh one.
//   the LOAD failed  - no. Nothing was clicked and there is no control to
//                      shake; the dialog simply has nothing to show, and that
//                      is a standing fact about its contents, not a refused
//                      action. It stays where the contents would have been, in
//                      the house's own ErrorCard.
function HosterPresetDialog({ host, base, onClose }: { host: string; base: string; onClose: () => void }) {
  const { t } = useT();
  const { toast } = useToast();
  const [preset, setPreset] = useState<YtdlpHosterPreset | null>(null);
  const [qualities, setQualities] = useState<string[]>([]);
  const [audioFormats, setAudioFormats] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState('');
  // Counted, and read as the Save button's `key`: an animation already at rest
  // does not replay because a class left and came back in one frame, so a
  // second identical refusal needs a fresh DOM node to shake.
  const [shake, setShake] = useState(0);

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
        if (live) setLoadError(err instanceof Error && err.message ? err.message : String(err));
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
      toast(err instanceof Error && err.message ? err.message : String(err), 'fail');
      setShake((n) => n + 1);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title={`${t('collector.hosterPreset')} · ${host}`}
      onClose={onClose}
      footer={
        <Button key={shake} className={shake > 0 ? 'glim-shake' : ''} onClick={() => void save()} disabled={!preset || saving}>
          {t('settings.save')}
        </Button>
      }
    >
      {loadError ? (
        <ErrorCard nested message={loadError} />
      ) : !preset ? (
        <LoadingCard nested label={t('common.loading')} />
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

// PackageRow is the folder header: a plain block inside the list card, not a
// nested card. Its totals are over every link in the package, folded or not: a
// collapsed package that stops counting turns the header into a number that
// changes when you click a chevron.
//
// It draws the header and nothing else. Its links are siblings of it in the
// table rather than children (see ListRow and TaskListCard's own `rows`): the
// table is one flat run of rows, because only a flat run can be windowed, and
// windowing is what keeps a list of several thousand links usable at all -
// measured, see useRowWindow. The tree is still a tree; it is drawn by the
// indent on the name cell rather than by the nesting of the elements, which is
// where it was already drawn from (see TREE_INDENT).
function PackageRow({
  name,
  items,
  base,
  ctx,
  columns,
  selection,
  collapsed,
  onToggleCollapsed,
  divider,
  dnd,
  onOpenProperties,
  current,
  onKeyDown,
  level,
  posinset,
  setsize,
}: {
  name: string;
  items: Task[];
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  selection?: Selection;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  /** The rule between one package and the next. It used to be `divide-y` on
   *  the container of the <section> elements, which is not available to a
   *  flat run of rows: a divider between every pair of ROWS would draw a line
   *  under every link. So the seam is a property of the header that opens a
   *  package, and the very first row of the table has none. */
  divider: boolean;
  /** See TaskRow's own identical prop: one press, and this row does not know
   *  what it means. */
  dnd: RowDnD;
  /** TaskRow's own onOpenProperties, for a double-click on the package
   *  header itself. */
  onOpenProperties?: () => void;
  /** See TaskRow's own identical prop. */
  current: boolean;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
  level: number;
  posinset: number;
  setsize: number;
}) {
  const allSelected = selection && items.every((x) => selection.ids.has(x.id));
  const unit: RowDragKey = { kind: 'package', name };
  const dragging = dnd.moving(unit);
  const ytdlpHost = items.find((x) => variantKindOf(x) && x.host)?.host;

  return (
    <div
      // The name is the only identity a package has on the wire, and it is
      // legitimately empty for the ungrouped one — so the attribute is present
      // and empty rather than absent, and the page tells the two apart.
      data-package-row={name}
      // See TaskRow's own pair: what useRowWindow measures this row by.
      data-row-key={rowKey({ kind: 'package', name })}
      data-row-kind="package"
      role="treeitem"
      tabIndex={current ? 0 : -1}
      aria-selected={selection ? !!allSelected : undefined}
      aria-expanded={!collapsed}
      aria-level={level}
      aria-posinset={posinset}
      aria-setsize={setsize}
      onKeyDown={onKeyDown}
      // Slid out of the way by a drag in flight exactly like a link row - see
      // TaskRow's own identical style above, and previewOffsets for the
      // arithmetic. This is the half jdp was missing (2026-09-03: "die ordner
      // über die man drüber hoovert müssen live verrutschen"): a folder header
      // is a row like any other here, so the folders a drag passes step aside
      // while the pointer is still down.
      // --row-ground: see TaskRow's own identical pair. A folder header rests on
      // the quiet surface rather than on nothing, so its own resting ground is a
      // real colour here and not `transparent`.
      style={{
        ...ROW_GRID,
        ...(allSelected ? { '--row-ground': SELECTED_GROUND } : null),
        ...dnd.slide(unit),
      }}
      // See TaskRow's own identical pair. A press on a folder header selects
      // the whole folder - the twisty button beside the name is CONTROL's own
      // match, so it keeps folding and unfolding on its own click.
      onPointerDown={(e) => dnd.press(e, unit)}
      onDoubleClick={(e) => {
        if (e.target instanceof Element && e.target.closest(CONTROL)) return;
        onOpenProperties?.();
      }}
      // A colour step, not a rule: the header sits on the quiet surface and
      // the links inside it sit on the card, which is the whole of the weight
      // difference between a container and its contents. The selected state
      // REPLACES the quiet ground rather than sitting alongside it, and it is
      // now one property doing it: this row paints --row-ground and nothing
      // else, so a selected header cannot lose a race between two background
      // utilities the way it once did (the quiet one won in Tailwind's
      // generated stylesheet order and a selected package was invisible).
      // `group` IST DER SCHALTER FUER DEN STREIFEN AM ZEILENENDE, und sein
      // Fehlen hat den Loeschknopf dieser Zeile gebaut und unsichtbar gemacht:
      // der Streifen steht auf opacity-0 und kommt ueber group-hover, und ohne
      // diese Klasse gibt es keinen Vorfahren, auf den sich das beziehen kann.
      // Die Linkzeile hatte sie von Anfang an, diese nie, weil sie bis dahin
      // nichts zu zeigen hatte.
      // The quiet surface and the hover step are both read off --row-ground now,
      // so the strip at the trailing edge wears exactly what this row paints -
      // see TaskRow's own note for the three rounds that took. The resting mix is
      // the colour `bg-carbon-surface2/80` composited to over the card, written
      // opaque so the strip can repeat it rather than stack a second 80% on it:
      // the strip on THIS row is drawn at rest as well (the collector's gear),
      // which is the state a hover-only ground would have got wrong.
      className={`group relative grid cursor-pointer select-none items-center ${
        allSelected ? 'glim-row-selected' : ''
      } ${divider ? 'border-t border-carbon-border/60' : ''} px-3 py-2.5 transition-colors
        bg-[var(--row-ground)]
        [--row-ground:color-mix(in_srgb,var(--carbon-surface2)_80%,var(--carbon-surface))]
        hover:[--row-ground:var(--carbon-surface2)]
        has-[:focus-visible]:[--row-ground:var(--carbon-surface2)] ${dragging ? 'opacity-50' : ''}`}
    >
      {columns.map((col) => (
        <div
          key={col.id}
          className={`min-w-0 truncate px-2 text-[12px] text-carbon-textSub ${
            col.align === 'end' ? 'text-end' : col.align === 'center' ? 'text-center' : 'text-start'
          } ${col.numeric ? 'glim-num' : ''}`}
        >
          {col.id === 'name' ? (
            <PackageName
              name={name}
              items={items}
              collapsed={collapsed}
              onToggle={onToggleCollapsed}
              focusable={current}
            />
          ) : (
            col.aggregate?.(items, ctx)
          )}
        </div>
      ))}

      {/* The gear badge for a package whose own "Variante" rows share a host
          (variantKindOf is '' for anything not yt-dlp-routed), floating over
          the trailing edge like a link row's own strip rather than owning a
          track: there is no actions track any more.

          Collector only (jdp, 2026-09-07: "im downloadtab soll dieser
          einstellungsbutten nicht erscheinen. das soll nur im sammlertab
          sein"). What it opens is which variants to keep and at what quality,
          and that is a decision about a link BEFORE it is fetched: once the
          rows are in the queue the choice has already been made, and offering
          it there is offering to change something that is on its way. */}
      {/* Der Streifen am Ende der Ordnerzeile, jetzt fuer zwei Dinge statt fuer
          eines. Er erscheint beim Zeigen auf die Zeile, genau wie der Streifen
          einer Linkzeile, damit beide Zeilenarten sich gleich verhalten - das
          Zahnrad bleibt die Ausnahme und steht auch ohne Zeigen da, weil es
          eine Einstellung anzeigt und nicht nur eine Handlung anbietet. */}
      {(ytdlpHost && ctx.profile === 'collector') || ctx.onRemovePackage ? (
        <div
          className={`absolute inset-y-px end-2 z-10 flex items-center gap-1 rounded-[var(--radius-control)]
            bg-[var(--row-ground)] px-1 transition-opacity
            ${ytdlpHost && ctx.profile === 'collector' ? '' : 'opacity-0 group-hover:opacity-100 [&:has(:focus-visible)]:opacity-100'}`}
        >
          {ytdlpHost && ctx.profile === 'collector' && (
            <HosterPresetButton host={ytdlpHost} base={base} focusable={current} />
          )}
          {/* Loescht NICHT selbst: es stellt dieselbe Frage, die eine
              Mehrfachauswahl stellt, mit Zaehlung, Dateiwahl und Rueckgaengig.
              Eine eigene Loeschstrecke hier waere eine zweite Stelle, an der
              sich dieselbe Frage spaeter auseinanderentwickeln kann. */}
          {ctx.onRemovePackage && (
            <IconBadge
              quiet
              hue={4}
              tabIndex={current ? 0 : -1}
              icon={<IconTrash width={16} height={16} />}
              title={ctx.t('task.remove')}
              aria-label={ctx.t('task.remove')}
              onClick={() => ctx.onRemovePackage?.(items.map((x) => x.id))}
            />
          )}
        </div>
      ) : null}
    </div>
  );
}

/** The bundle TaskRow and PackageRow share, built once per render in TaskListCard. */
interface RowDnD {
  /** Whether this row is one of the rows a move in flight is carrying. A move
   *  carries the whole selection, so this is a set and not one key. */
  moving: (unit: RowDragKey) => boolean;
  /** The press. Everything it can turn into - a click, a selection sweep, a
   *  move - is decided by TaskListCard; see its own gesture section. */
  press: (e: PointerEvent<HTMLElement>, unit: RowDragKey) => void;
  /** How far this row has to slide to show where the move in flight would put
   *  it, as the inline style that does it - an empty object when no move is
   *  running. Every link row and every folder header spreads this into its own
   *  style; see TaskListCard's previewOffsets for where the numbers come from. */
  slide: (unit: RowDragKey) => CSSProperties;
}

/** The three fields TaskListCard's own selectUnit reads off a press. */
type SelectMods = { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean };
const PLAIN: SelectMods = { ctrlKey: false, metaKey: false, shiftKey: false };

/**
 * A press that has not finished being one thing or another yet.
 *
 * `mode` is settled at the press itself and never changes afterwards, which is
 * the whole of what makes the gesture learnable: what a press will do is
 * decided by what was under it, not by how far it later travels. What the
 * distance decides is only WHETHER it happens at all (see pastThreshold).
 */
interface Gesture {
  /** 'select' sweeps a range, 'move' carries the selection. */
  mode: 'select' | 'move';
  /** The row the press landed on. */
  unit: RowDragKey;
  /** Its place in the on-screen order, which is where the sweep measures its
   *  own direction from. NOT the same thing as the range anchor: a Shift-press
   *  lands somewhere the anchor is not. */
  fromIndex: number;
  pointerId: number;
  /** Where the press was, in client coordinates - the threshold measures from
   *  here, never from the previous move. */
  fromX: number;
  fromY: number;
  /** Where the pointer is now. Read by the edge scroll, which has to know where
   *  the pointer is at a moment when the pointer is not moving. */
  atX: number;
  atY: number;
  /** The keys held at the PRESS. A modifier taken away mid-sweep does not
   *  change what the sweep is doing, the same way letting go of Shift halfway
   *  through a Shift-click does not undo it. */
  mods: SelectMods;
  /** The selection as the press found it: what Escape puts back, and what a
   *  Ctrl-sweep adds to. */
  before: ReadonlySet<string>;
  /** Whether the pointer has travelled far enough for this to be a gesture at
   *  all rather than a click. */
  live: boolean;
  /** The rows a move is carrying, in drawn order. Empty until `live`. */
  block: RowDragKey[];
  /** The last row a sweep selected up to, so a pointer wandering inside one row
   *  does not rewrite the selection sixty times a second. */
  sweptTo: number | null;
  /** The aim a move would commit right now. Kept here as well as in state
   *  because the drop reads it in the same tick the last move wrote it. */
  over: { target: RowDragKey; after: boolean } | null;
}

/** Which drag unit a drawn row element stands for, or null for anything that is
 *  not a row (the two window spacers, the keyboard's own probe). */
function unitOfRow(el: HTMLElement): RowDragKey | null {
  if (el.dataset.taskId !== undefined) return { kind: 'task', id: el.dataset.taskId };
  if (el.dataset.packageRow !== undefined) return { kind: 'package', name: el.dataset.packageRow };
  return null;
}

// How close to the edge of the scrolling box the pointer has to come before the
// list starts moving under it, and how fast it moves at the very edge.
//
// THE EDGE SCROLL IS NOT A FLOURISH: the native HTML5 drag this gesture
// replaced got it from the browser for free, and a list that cannot be dragged
// past the bottom of the window is a list where a folder can only ever be moved
// as far as one screen. 56px is a little over a row and a half, so the band is
// reachable without being somewhere the pointer sits by accident; 18px a frame
// is roughly a screenful every two seconds at the very edge, and it eases in
// across the band so that entering it does not lurch.
const EDGE_BAND_PX = 56;
const EDGE_SPEED_PX = 18;

/** The box that actually scrolls this list, or null when it is the page. */
function scrollerOf(el: HTMLElement | null): HTMLElement | null {
  for (let n = el?.parentElement ?? null; n; n = n.parentElement) {
    const overflow = getComputedStyle(n).overflowY;
    if ((overflow === 'auto' || overflow === 'scroll') && n.scrollHeight > n.clientHeight + 1) return n;
  }
  return null;
}

/**
 * The 8px grab strip on a column's trailing edge.
 *
 * Its own component only so it can hold a hook: it carries the house bubble
 * rather than a native `title=` (ONE CONTROL, ONE TOOLTIP MECHANISM - see
 * columns.tsx's `Tip`), and a hook cannot live inside the header's own
 * `map()`. The handle is invisible furniture with no text of its own, which is
 * the case GlimStone says needs a tooltip unconditionally.
 */
function ResizeHandle({
  hint,
  onResize,
  onReset,
}: {
  hint: string;
  onResize: (phase: 'start' | 'move' | 'end', e: PointerEvent<HTMLElement>) => void;
  onReset: () => void;
}) {
  const tip = useTooltip<HTMLSpanElement>(hint);
  // role/tabIndex dropped: this element already states role="separator", and
  // the "note" role the hook hands out would replace it.
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;
  return (
    <>
      <span
        role="separator"
        aria-orientation="vertical"
        aria-label={hint}
        {...hover}
        onPointerDown={(e) => onResize('start', e)}
        onPointerMove={(e) => onResize('move', e)}
        onPointerUp={(e) => onResize('end', e)}
        // A drag the browser takes away (a context menu, a window switch)
        // must still settle the width, or the table keeps a hand-painted
        // track list that the next render silently undoes.
        onPointerCancel={(e) => onResize('end', e)}
        onDoubleClick={onReset}
        className="absolute inset-y-0 end-0 z-10 w-2 cursor-col-resize touch-none"
      >
        {/* The visible line, drawn inside the 8px grab area rather than
            as a border on the cell: a border would sit at the edge of the
            COLUMN, and the thing to aim at is the handle. Transparent
            until the header is hovered, the accent once it is, and always
            ignoring the pointer so it never eats the drag it advertises. */}
        <span
          aria-hidden
          className="pointer-events-none absolute inset-y-1 end-[3px] w-px bg-transparent
            transition-colors group-hover/header:bg-carbon-border"
        />
      </span>
      {tip.node}
    </>
  );
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
  profile,
  sort,
  onSort,
  onReorder,
  onResize,
  onResizeReset,
  onMenu,
}: {
  layout: ResolvedLayout;
  profile: ListProfile;
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
      // group/header: hovering anywhere on the header lights up EVERY column
      // boundary at once (jdp, 2026-09-07: "beim mouseover auf die kopfleiste
      // sollen die spalten grenzen aufleuchten damit man sie besser sieht und
      // nicht suchen muss"). Per-handle hover would not do: the handle is 8px
      // wide and invisible, so you cannot hover what you are still looking for.
      // The whole row is the target, and the answer is every line at once.
      className="group/header relative grid items-center border-b border-carbon-border/60 px-3 py-1 select-none"
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
              {/* One list can call a column something else - see
                  CellContext's own `profile` doc comment for why that is one
                  column and not two. */}
              <span className="truncate">{t(col.labelByProfile?.[profile] ?? col.labelKey)}</span>
              {sorted === 'asc' && <IconArrowUp width={11} height={11} className="shrink-0" />}
              {sorted === 'desc' && <IconArrowDown width={11} height={11} className="shrink-0" />}
            </button>

            {/* Double-click gives a column its built-in width back, which is the
                only way out of a column dragged down to its minimum. */}
            <ResizeHandle
              hint={t('columns.resizeHint')}
              onResize={(phase, e) => onResize(col.id, phase, e)}
              onReset={() => onResizeReset(col.id)}
            />
          </div>
        );
      })}

      {/* No bubble in this row any more. It explained the whole header at once
          - right-click for the column menu, click a label to sort - and it
          moved to the card's own title badge (jdp, 2026-09-06: "die können wir
          ja in den cardtitelbadge machen"), where TaskListCard renders it. The
          header row now holds nothing but the column labels and their resize
          handles, which is what a header row is.

          Its history, kept because it is a record of two earlier corrections:
          it was one bubble for the whole row rather than one per column, and it
          was moved from a leading gutter to the trailing edge (jdp,
          2026-08-26) so the table could shift left into the space the checkbox
          column used to own. The gutter it lived in is gone either way, since
          the last column now runs to the right edge (jdp, 2026-09-06: "im
          downloadtab ist rechts eine spalte die leer ist"). */}
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
export function TaskProperties({
  ids,
  tasks,
  base,
  autoFocus = false,
}: {
  ids: string[];
  tasks: Task[];
  base: string;
  /** Set only where the KEYBOARD opened this panel. It renders below the list
   *  card, which on a five-thousand-row list is thousands of pixels down the
   *  page: Enter would otherwise open a panel nobody can see. A double-click
   *  never sets it - a mouse user's focus is theirs. */
  autoFocus?: boolean;
}) {
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
  // The Save button's own failure counter, read as its `key`. A refused save
  // reports through the two channels every action here uses - the server's
  // sentence in a toast, the pressed control shaking - and never as a sentence
  // left standing in the row beside the button, which nothing ever clears.
  const [shake, setShake] = useState(0);

  const panelRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!autoFocus) return;
    panelRef.current?.focus({ preventScroll: true });
    panelRef.current?.scrollIntoView({ block: 'nearest' });
  }, [autoFocus]);

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
    const r = await setTaskOptions(ids, opts, base);
    setBusy(false);
    if (!r.ok) {
      // These routes refuse with a sentence, and the sentence is the whole point
      // of refusing: a rename that could not happen has a reason, and hiding it
      // behind "save failed" leaves the row promising a name the folder does not
      // have. It goes to the toast, which is where a failure is read.
      toast((await r.text()).trim() || t('list.optionsFailed'), 'fail');
      setShake((n) => n + 1);
      return;
    }
    setTouched(new Set());
    toast(t('settings.saved'), 'ok');
  }

  return (
    // The right-click belongs to the browser in here. The page above this puts a
    // context menu on the whole list area and calls preventDefault on every
    // reading of it, which inside a text box means no paste entry.
    <section
      ref={panelRef}
      tabIndex={-1}
      aria-label={t('props.title')}
      onContextMenu={(e) => e.stopPropagation()}
    >
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

            IT WRAPS, IT NEVER SCROLLS. The strip used to sit in an
            `overflow-x-auto` box here, on the reasoning that the well pins
            every segment to a flat 200px and priority's own seven of them run
            wider than this grid column. Both halves of that are out of date:
            the small scale drops the pinning (Tabs.tsx's wellWidth: the `sm`
            track measures its own labels), and the track itself wraps
            (Tabs.tsx: "Wraps, never scrolls", added for THIS selector - jdp,
            2026-08-25: "die buttons so breit machen dass kein horizontaler
            scrollbar notwendig ist"). A wrapper outside the component put the
            horizontal scrollbar back one level up, which is the gesture the
            language rules out; without it the strip grows in height instead. */}
        <div className="grid gap-4 sm:grid-cols-2">
          <FieldGroup
            label={t('props.priority')}
            hint={hint(t('props.priorityHint'), start.priority === null)}
          >
            <Tabs
              variant="well"
              size="sm"
              className="w-fit"
              label={t('props.priority')}
              active={priority}
              onSelect={edit('priority', setPriority)}
              items={priorities.map((p) => ({ id: p.id, label: t(p.label) }))}
            />
          </FieldGroup>

          <FieldGroup
            label={t('props.autoExtract')}
            hint={hint(t('props.autoExtractHint'), start.autoExtract === null)}
          >
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
          </FieldGroup>
        </div>

        <div className="flex items-center gap-3">
          <Button
            key={shake}
            className={shake > 0 ? 'glim-shake' : ''}
            disabled={touched.size === 0 || busy}
            onClick={() => void apply()}
          >
            {t('settings.save')}
          </Button>
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
  hue,
  revealKey,
  onRemovePackage,
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
  /** This card's own rainbow position, independent of whatever hues the
   *  caller's own hero row or badge row already used - see Collector.tsx's
   *  and Downloads.tsx's own call sites for why each picks a different
   *  number. */
  hue?: number;
  /**
   * "Scroll to this row, once." A row key plus a nonce ("task:abc#7"), never a
   * bare id: the guard below handles each distinct value exactly once, so
   * without the nonce revealing the same row twice running is a no-op the
   * second time. See lib/reveal.ts.
   */
  revealKey?: string;
  /** Siehe CellContext.onRemovePackage - die Seite stellt die Frage, nicht die Zeile. */
  onRemovePackage?: (ids: string[]) => void;
}) {
  const { t } = useT();
  // Only the row move reports through this so far (see dropBlock and
  // beginGesture): the queue refuses a reorder with a sentence, and a move that
  // is refused in silence reads as a move the app never received.
  const { toast } = useToast();
  // One subscription for the whole table rather than one per row: the palette
  // changes for every row at once anyway.
  useRainbow();

  const [stored, setStored] = useUIState<ColumnLayout | null>(`list.columns.${profile}`, null);
  const [storedSort, setSort] = useUIState<SortState | null>(`list.sort.${profile}`, null);
  const { collapsed, collapse, expand, toggle } = useCollapsedPackages(profile);
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
  // Whether the panel about to appear should take the focus. True only on the
  // Enter path: on a five-thousand-row list the panel renders thousands of
  // pixels below the row that opened it, so "Enter opens the properties" is
  // otherwise true and completely invisible to whoever pressed it. Never on the
  // double-click path - a mouse user has not asked for their focus to be moved.
  const [propertiesAutoFocus, setPropertiesAutoFocus] = useState(false);

  const tableRef = useRef<HTMLDivElement>(null);
  // The rows themselves, without the header row above them or the spacer below:
  // what useRowWindow measures, and what a drag snapshot reads. Separate from
  // tableRef because the header would otherwise count as part of the strip's own
  // top edge, and the window's arithmetic is in the strip's coordinates.
  const stripRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ id: ColumnId; startX: number; startWidth: number; width: number } | null>(null);

  const layout = useMemo(() => resolveLayout(profile, stored), [profile, stored]);
  const ctx = useMemo<CellContext>(
    () => ({ t, base, profile, onRemovePackage }),
    [t, base, profile, onRemovePackage],
  );

  // A sort on a column that is currently hidden is ignored rather than cleared,
  // so showing the column again brings the order back with it. Applying it while
  // its column is invisible would be a list in an order with nothing on screen
  // to explain it.
  const sort = storedSort && !layout.hidden.has(storedSort.id) ? storedSort : null;
  const view = useMemo(() => applySort(groups, sort), [groups, sort]);

  // The table, flattened: every folder header, and - only while that folder is
  // open - its own links, in the order they are drawn. Everything downstream
  // reads this rather than walking the packages again for itself, so the window,
  // the Shift-range and the rows on screen cannot describe three different
  // lists. See ListRow.
  const rows = useMemo<ListRow[]>(() => {
    const out: ListRow[] = [];
    let hue = 0;
    // The sibling-set numbers a screen reader reads out, counted here where the
    // grouping is still in hand: a folder is one of the folders, a link is one
    // of ITS OWN folder's links. Counted off the window instead they would say
    // "3 of 40" on a list of five thousand, which is a worse answer than none.
    const packages = view.length;
    let pkgAt = 0;
    for (const [name, items] of view) {
      const folded = collapsed.has(name);
      out.push({
        kind: 'package',
        key: rowKey({ kind: 'package', name }),
        name,
        items,
        collapsed: folded,
        divider: out.length > 0,
        level: 1,
        posinset: ++pkgAt,
        setsize: packages,
      });
      if (!folded) {
        items.forEach((x, i) =>
          out.push({
            kind: 'task',
            key: rowKey({ kind: 'task', id: x.id }),
            task: x,
            index: hue++,
            level: 2,
            posinset: i + 1,
            setsize: items.length,
          }),
        );
      }
    }
    return out;
  }, [view, collapsed]);

  // --- Click-to-select (jdp, 2026-08-26: "in der linkliste soll man
  // links und ordner mit einem klick markieren können, nicht den ordner
  // aufklappen. die checkbox spalte können wir wegmachen. mehrere links
  // oder ordner soll man mit klick und strg oder umschalttaste auswählen
  // können. wie in windows") -------------------------------------------
  //
  // selectableOrder is the flat, on-screen order a Shift-click's range walks,
  // which is `rows` above with each entry's own ids attached. A collapsed
  // package contributes only itself; Shift-clicking across one selects the whole
  // folded package as a single step, the same as if its rows had never been
  // individually visible to click between.
  //
  // Every row the list HOLDS, never only the ones the window has drawn: a range
  // that stopped at the edge of the viewport would select a different set
  // depending on how far somebody had scrolled, which is not a rule anybody
  // could learn.
  const selectableOrder = useMemo(
    () =>
      rows.map((r) =>
        r.kind === 'package'
          ? { kind: 'package' as const, key: r.name, ids: r.items.map((x) => x.id) }
          : { kind: 'task' as const, key: r.task.id, ids: [r.task.id] },
      ),
    [rows],
  );

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

  /** Every id between two places in the on-screen order, both ends included.
   *  Shared by the Shift-range and the press-and-sweep, which have to agree
   *  about what a range IS or the mouse contradicts itself. */
  function rangeIds(a: number, b: number): Set<string> {
    const lo = Math.min(a, b);
    const hi = Math.max(a, b);
    const range = new Set<string>();
    for (let i = lo; i <= hi; i++) selectableOrder[i]?.ids.forEach((id) => range.add(id));
    return range;
  }

  function selectUnit(kind: 'task' | 'package', key: string, ids: string[], e: SelectMods): void {
    if (!selection) return;
    const index = selectableOrder.findIndex((u) => u.kind === kind && u.key === key);
    if (index < 0) return;
    if (e.shiftKey && selectAnchor.current !== null) {
      selection.set(rangeIds(selectAnchor.current, index));
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

  // --- One press, three meanings ---------------------------------------
  //
  // THE GESTURE IS JDOWNLOADER'S, and it is jdp's own description of it (jdp,
  // 2026-09-14: "ich möchte das drag and drop von links und ordnern wie in JD.
  // wennman ein ordner anklickt und geklickt hält und nach unten zieht, markiert
  // man die links und ordner. wenn man einmal draufklickt markiert man, dann ein
  // zweiter klick den man hält und man kann per drag and drop verschieben"):
  //
  //   press on an UNMARKED row, then drag  -> sweep a selection over the rows
  //                                           the pointer passes,
  //   press and let go                     -> mark that one row,
  //   press on a MARKED row, then drag     -> move THE WHOLE MARKING.
  //
  // It is not an invention of jdp's either: it is what Swing hands every table
  // that switches drag on, and JDownloader's download table is one
  // (DownloadsTable calls setDragEnabled and setTransferHandler; verified in the
  // shipped bytecode of the JDownloader running on the server, rather than from
  // memory). BasicTableUI's own canStartDrag() is one line - "is the pressed
  // cell already selected" - and everything else a press could mean falls
  // through to changeSelection(row, col, false, true), which extends the range
  // from the anchor. So the rule jdp described from a chair is literally the
  // rule the toolkit implements, down to the anchored range.
  //
  // WHY THIS COULD NOT STAY ON HTML5 DRAG. A native drag takes the pointer over
  // the instant it starts, and there is no way to hold it back until the app has
  // decided whether the press was a sweep or a move - dragstart arrives before
  // the pointer has travelled anywhere it could be judged by. So `draggable` is
  // gone from the rows and this runs on pointer events. What that buys, beyond
  // the gesture:
  //
  //   - POINTER CAPTURE. Every move and the release come to the strip whatever
  //     is painted under the pointer, which is what the stationary sheet over
  //     the rows used to fake. A row sliding out from under a still pointer can
  //     no longer swallow the drop, so the sheet is gone with it.
  //   - The hit test can read the pointer's own Y and nothing else, which it
  //     already wanted to (see aimAt).
  //
  // What it costs, and what is paid back below:
  //
  //   - the browser's own edge scrolling during a drag (see EDGE_BAND_PX),
  //   - a press that must not be swallowed by a text selection (select-none on
  //     the rows) or by the native drag of an <img> inside one (the strip's own
  //     onDragStart).
  //
  // TOUCH IS DELIBERATELY LEFT TO THE BROWSER. A finger pressed on a list and
  // dragged down means "scroll" everywhere else on this page and on every other
  // page; taking that over would cost the only way to move down a long list to
  // buy a gesture that has a keyboard and a context menu as alternatives. The
  // phone has its own list with its own long-press drag (mobile's DragList), so
  // nothing is lost that was ever here. A stylus (pointerType 'pen') is a mouse
  // for this purpose and does get the gesture.
  //
  // Only MOVED in queue-order view — a client-side sort is documented above
  // (applySort's own doc comment) as a VIEW and never the queue itself, and
  // band-mates a size or status sort has scattered across the table would rarely
  // even land next to each other to drag between. The sortedView banner right
  // above the table offers the way back, and a move attempted there is refused
  // OUT LOUD rather than being quietly impossible. SELECTING is never refused:
  // the marking has nothing to do with the order the rows are drawn in.
  const dndEnabled = !sort;
  // The rows a move in flight is carrying, in drawn order, or null when nothing
  // is being moved. Several units, because a move carries the whole marking.
  const [rowDrag, setRowDrag] = useState<RowDragKey[] | null>(null);
  // The row currently aimed at mid-move, and which half of it — updated on every
  // pointer move, not just the eventual drop. This is what lets the OTHER rows
  // actually move out of the way live instead of only snapping into their new
  // order once the mouse is released (jdp, 2026-08-25: "die elemente sollen live
  // verrutschen wenn ich zb ein link über einen anderen ziehe").
  const [dragOver, setDragOver] = useState<{ target: RowDragKey; after: boolean } | null>(null);
  const gesture = useRef<Gesture | null>(null);
  const edgeScroll = useRef<{ frame: number; box: HTMLElement | null }>({ frame: 0, box: null });

  // A frozen snapshot of every row's own position, taken once at the moment a
  // MOVE starts — see snapshotSlots() below for why this exists at all: without
  // it, "which row, which half" came from whichever DOM element the browser
  // delivered the event to, and that element itself moves the moment the live
  // preview slides it, feeding its own output back in as its next input (jdp,
  // 2026-08-25: "jetzt springen die einzelnen elemente... die ganze zeit hin und
  // her"). A snapshot the preview never touches breaks that loop.
  //
  // It is the ONE geometry the whole move runs on: the list keeps rendering its
  // resting order for as long as the pointer is down and every row is slid to
  // its previewed place by a transform (previewOffsets below), so the document
  // flow the snapshot measured is still the true one at every moment of the
  // move. The hit test and what the eye sees cannot drift apart, because the
  // second of them is computed FROM the first.
  //
  // IN THE STRIP'S OWN COORDINATES, not the viewport's, and that changed with
  // the edge scroll: the snapshot has to survive the list scrolling under the
  // pointer, and a box measured against the window stops describing the row the
  // moment anything scrolls. The strip scrolls with its rows, so a top measured
  // from the strip's own top stays true; the pointer is converted into the same
  // frame on every read. stackOffsets works on differences and never cared which
  // origin it was handed.
  //
  // In DOM order, and that matters: previewOffsets stacks rows back up in this
  // order and needs the gap between each pair, not only their own boxes.
  //
  // On a long list this is the WINDOW's rows and not the whole table, because
  // that is what the DOM holds (see useRowWindow) - and it is also exactly the
  // right set. Every row a move can aim at is a row somebody can see, so a hit
  // test over the window answers the same question the full table would; and a
  // row nobody can see has nothing to show by stepping aside. previewOffsets
  // below is written against that: it previews the rows it measured, rather than
  // insisting the two lists have the same length. The one place that shows is a
  // move that edge-scrolls a WINDOWED list far enough to draw rows the snapshot
  // never measured: the preview then bails out whole, exactly as it does for a
  // poll that adds a row, and the drop still lands where the last honest aim
  // pointed. That is the same graceful stop as before and strictly better than
  // the native drag, which scrolled against a viewport snapshot and went wrong
  // on any scroll at all.
  const rowSlotsRef = useRef<RowSlot[]>([]);

  function snapshotSlots(): void {
    const root = stripRef.current;
    if (!root) {
      rowSlotsRef.current = [];
      return;
    }
    const origin = root.getBoundingClientRect().top;
    const slots: RowSlot[] = [];
    root.querySelectorAll<HTMLElement>('[data-task-id],[data-package-row]').forEach((el) => {
      const unit = unitOfRow(el);
      if (!unit) return;
      const r = el.getBoundingClientRect();
      slots.push({ unit, top: r.top - origin, bottom: r.bottom - origin });
    });
    rowSlotsRef.current = slots;
  }

  /** The pointer's Y in the frame snapshotSlots measured in. */
  function stripY(clientY: number): number {
    const root = stripRef.current;
    return root ? clientY - root.getBoundingClientRect().top : clientY;
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

  // Every drawn row with the movable ids it stands for - what selectedBlock
  // walks to work out what a move carries. A folder contributes its own movable
  // links, which is what lets a folder holding one finished file still travel
  // as a folder.
  const blockRows = useMemo<BlockRow[]>(
    () =>
      rows.map((r) =>
        r.kind === 'package'
          ? { unit: { kind: 'package', name: r.name }, ids: r.items.filter(movable).map((x) => x.id) }
          : { unit: { kind: 'task', id: r.task.id }, ids: [r.task.id] },
      ),
    // movable is a pure helper over its own argument, so rows is the only input.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [rows],
  );

  /** Every id a unit stands for, movable or not - what the MARKING covers, as
   *  against unitIds, which is what the queue can be told to move. A folder
   *  header paints as selected only when all of its links are, finished ones
   *  included, so the press has to ask the same question the paint does. */
  function unitAllIds(u: RowDragKey): string[] {
    if (u.kind === 'task') return [u.id];
    return (view.find(([n]) => n === u.name)?.[1] ?? []).map((x) => x.id);
  }

  /** selectUnit, addressed by drag unit rather than by kind and key. */
  function selectFromUnit(u: RowDragKey, mods: SelectMods): void {
    if (u.kind === 'task') selectUnit('task', u.id, [u.id], mods);
    else selectUnit('package', u.name, unitAllIds(u), mods);
  }

  /**
   * What a move started on `pressed` carries.
   *
   * THE WHOLE MARKING, not the row the hand grabbed (jdp: "und zwar die ganze
   * Markierung"). With no selection model at all, or with a marking the pressed
   * row has somehow fallen out of between the press and the first move (a poll
   * can remove a task mid-gesture), it falls back to the one row - a move that
   * carries nothing would be the gesture doing nothing, which is the exact
   * failure this whole file has been chasing.
   */
  function blockFor(pressed: RowDragKey): RowDragKey[] {
    const marked = selection?.ids;
    if (!marked || marked.size === 0) return [pressed];
    const block = selectedBlock(blockRows, marked);
    return block.length > 0 ? block : [pressed];
  }

  /**
   * The drop: `block` lands against `target`'s given half, whatever bands and
   * folders that crosses.
   *
   * ONE FUNCTION FOR WHAT USED TO BE FOUR (reorderedBand, crossBandOrder,
   * dropAcrossBands, dropRow), and the reason is the gesture: a marking is
   * allowed to hold rows from two priority bands and from three folders at once,
   * so "same band" and "different band" stopped being two cases and became one
   * question asked per row. The splice below is band-blind - it takes the TARGET
   * band's order, drops whatever the block holds out of it, and puts the block
   * back at the anchor - which is exactly right for both, because a row already
   * in that band is filtered out and re-inserted and a row from elsewhere is
   * simply inserted.
   *
   * WHAT A MIXED MARKING DOES, and why. The list is ordered by priority before
   * anything else (the server's own bands), so rows of two priorities cannot
   * come to rest next to each other while keeping their priorities: either the
   * drop is refused or the priority changes. Refusing would mean a marking made
   * with one Shift-click across a band boundary can never be moved, and nothing
   * on screen would explain why - which is what "das drag and drop funktioniert
   * überhaupt nicht" has meant every previous time. So the rows take the
   * priority of the row they were dropped on, exactly as a single row already
   * did before this gesture existed, and the toast says so: a priority quietly
   * changing under a drag would be worse than the drag doing nothing, and only
   * the rows that actually changed are counted.
   *
   * Loose links join the folder they were dropped into, the way they do in
   * JDownloader - but a link that is travelling as part of its OWN folder does
   * not, because that folder is moving whole and its links are going with it.
   */
  function dropBlock(block: readonly RowDragKey[], target: RowDragKey, after: boolean): void {
    const band = unitBand(target);
    if (!band) return;
    const movedIds = block.flatMap(unitIds);
    const targetIds = unitIds(target);
    if (movedIds.length === 0 || targetIds.length === 0) return;
    // Dropped on itself, or on a part of itself. Not a failure, just not a move.
    const moving = new Set(movedIds);
    if (targetIds.some((id) => moving.has(id))) return;

    const before = bandOrder.get(band) ?? [];
    // A Set and not `movedIds.includes`: a marking can be thousands of rows on a
    // list of thousands, and that pair walked one against the other.
    const order = before.filter((id) => !moving.has(id));
    // Anchored on the target's own edge — its first id when the block lands
    // before it, its last when after — so a folder (several ids at once) keeps
    // its own internal order and lands as one contiguous run, exactly where a
    // single link would have landed alone.
    const anchor = after ? targetIds[targetIds.length - 1] : targetIds[0];
    const at = order.indexOf(anchor);
    if (at < 0) return;
    order.splice(after ? at + 1 : at, 0, ...movedIds);

    const dst = taskById.get(targetIds[0]);
    if (!dst) return;
    // The rows that have to change hands before the order above means anything.
    const reband = movedIds.filter((id) => {
      const x = taskById.get(id);
      return !!x && bandOf(x) !== band;
    });
    const home = target.kind === 'package' ? target.name : (taskById.get(target.id)?.package ?? '');
    const travellingWithTheirFolder = new Set(
      block.flatMap((u) => (u.kind === 'package' ? unitIds(u) : [])),
    );
    const rehome = movedIds.filter(
      (id) => !travellingWithTheirFolder.has(id) && (taskById.get(id)?.package ?? '') !== home,
    );

    // A DROP THAT LANDS WHERE IT STARTED IS NOT A CHANGE and must not write
    // anything. The refusals above are all about the drop being impossible; this
    // one is about it being pointless, and it needs its own test because the
    // splice can put every id back exactly where it was: drag a row onto the
    // upper half of the row directly below it, or onto the lower half of the one
    // directly above, and the result is the list it started from. That is the
    // SHORTEST movement the gesture can make, not an exotic edge case, and it
    // used to cost a POST and a write to the stored order. Compared by result
    // rather than by index, so it also catches a whole marking landing back on
    // its own footprint. Only when nothing else was going to happen either: a
    // re-band or a change of folder is a real move even at the same index.
    if (
      reband.length === 0 &&
      rehome.length === 0 &&
      order.length === before.length &&
      order.every((id, i) => id === before[i])
    ) {
      return;
    }

    // The move goes first and the reorder follows it: a row has to be IN the
    // band and IN the folder before its position among their rows means
    // anything. There is no local override of the task order to unwind if this
    // fails - the next poll or websocket tick is what settles rows back where
    // the server actually put them - but a refusal has to SAY something.
    // reorderTasks throws with the server's own sentence (api.ts, ok()), and
    // swallowing that made a rejected move indistinguishable from a move the app
    // never noticed, which is precisely how it was reported ("funktioniert
    // überhaupt nicht").
    void (async () => {
      try {
        if (rehome.length > 0) await setPackage(rehome, home, base);
        if (reband.length > 0) {
          const wrongPriority = reband.filter((id) => taskById.get(id)?.priority !== dst.priority);
          const wrongForced = reband.filter((id) => !!taskById.get(id)?.forced !== !!dst.forced);
          if (wrongPriority.length > 0) await setTaskPriority(wrongPriority, dst.priority, base);
          if (wrongForced.length > 0) await setTaskForced(wrongForced, !!dst.forced, base);
        }
        await reorderTasks(order, base);
        if (reband.length > 0) toast(t('list.dropChangedPriority', { n: reband.length }), 'info');
      } catch (err) {
        toast(t('list.failed', { error: err instanceof Error ? err.message : String(err) }), 'fail');
      }
    })();
  }

  // --- The press ---------------------------------------------------------
  //
  // WHAT EACH PRESS MEANS, and the four questions this had to answer before a
  // line of it was written. They are decided here, together, because deciding
  // them one at a time is how a gesture ends up with three different ideas of
  // what "marked" means:
  //
  //  1. SHIFT AND CTRL KEEP EVERYTHING THEY HAD. Shift extends the range from
  //     the anchor, Ctrl adds or removes one unit; neither ever starts a move.
  //     That last part is a deliberate departure from Swing, which would let
  //     Shift start one (its modifier map reads Shift as "move"): here Shift is
  //     the range key and nothing else, because a Shift-press on a row that
  //     happens to already be marked would otherwise mean "extend" or "carry"
  //     depending on something the person cannot see. A coin toss is not a
  //     gesture. Held down through a drag, both keep their meaning: Shift-drag
  //     extends the range live, Ctrl-drag ADDS the swept range to whatever was
  //     marked before the press.
  //
  //  2. A MARKING THAT SPANS TWO PRIORITY BANDS STILL MOVES, and everything in
  //     it takes the priority of the row it was dropped on. See dropBlock for
  //     the whole argument; the short version is that the alternative is a
  //     marking that cannot be moved for a reason nothing on screen explains.
  //
  //  3. A MARKED FOLDER TAKES ITS LINKS, always. There is no second question
  //     there: pressing a folder header marks every link in it (selectUnit), so
  //     a marked folder IS a folder whose links are marked, and selectedBlock
  //     emits the folder rather than its links one by one so that it lands as
  //     one run with its own order intact.
  //
  //  4. WHAT DROPS THE MARKING: a plain press on an unmarked row (it becomes the
  //     marking), a plain press on a marked row once it is RELEASED without
  //     travelling (it collapses to that one row), a sweep (it becomes the
  //     range), and Escape during a sweep (it goes back to what it was before
  //     the press). What does NOT: a poll or websocket tick, folding a folder,
  //     a right-click, a refused move, and - the one that makes the whole
  //     gesture possible - the PRESS on a marked row, which must leave the
  //     marking alone until it knows whether a move is coming. Pressing the
  //     empty space below the rows does not clear it either; that space belongs
  //     to the list's own context menu.
  //
  // The deferred collapse in (4) is Swing's too: BasicTableUI holds the
  // selection change back to mouseReleased whenever the press could have been a
  // drag, which is exactly why "click once to mark, then click and hold to
  // move" works there. Doing it at the press instead would destroy the marking
  // the second press is trying to pick up.
  function pressRow(e: PointerEvent<HTMLElement>, unit: RowDragKey): void {
    // Left button only, and one pointer at a time. A right-click belongs to the
    // context menu, which reads the row it landed on for itself.
    if (e.button !== 0 || !e.isPrimary) return;
    // Everything that is its own control keeps its own gesture: an action badge,
    // the Enabled switch, the folder twisty.
    if (e.target instanceof Element && e.target.closest(CONTROL)) return;
    // Touch scrolls. See the section head for why that is a decision and not an
    // omission.
    if (e.pointerType === 'touch') return;
    abortGesture();

    const mods: SelectMods = { ctrlKey: e.ctrlKey, metaKey: e.metaKey, shiftKey: e.shiftKey };
    const before: ReadonlySet<string> = new Set(selection?.ids ?? []);
    const ids = unitAllIds(unit);
    const marked = !!selection && ids.length > 0 && ids.every((id) => before.has(id));
    const mode: 'select' | 'move' =
      mods.ctrlKey || mods.metaKey || mods.shiftKey || !marked ? 'select' : 'move';

    // The cursor follows the pointer, so a later Tab into the list resumes from
    // the row the mouse last touched.
    keys.setCurrent(rowKey(unit));
    // A sweep marks its first row straight away - the press is already the first
    // step of the range, and a row that lights up under the finger is how the
    // gesture says it has begun. A move marks nothing yet; that is (4) above.
    if (mode === 'select') selectFromUnit(unit, mods);

    gesture.current = {
      mode,
      unit,
      fromIndex: selectableOrder.findIndex((u) =>
        unit.kind === 'task' ? u.kind === 'task' && u.key === unit.id : u.kind === 'package' && u.key === unit.name,
      ),
      pointerId: e.pointerId,
      fromX: e.clientX,
      fromY: e.clientY,
      atX: e.clientX,
      atY: e.clientY,
      mods,
      before,
      live: false,
      block: [],
      sweptTo: null,
      over: null,
    };
  }

  /** The press has travelled far enough to mean something. Returns false when
   *  the gesture is refused outright, in which case it is already over. */
  function beginGesture(g: Gesture, strip: HTMLElement): boolean {
    if (g.mode === 'move') {
      // A SORTED VIEW SAYS SO INSTEAD OF DOING NOTHING. Rows used to simply not
      // be draggable there, which from a chair is the same picture as a broken
      // list: you pick a folder up, nothing follows the pointer, and nothing
      // explains why. The banner above the table says the view is sorted; it has
      // never said that the order cannot be changed while it is.
      if (!dndEnabled) {
        toast(t('list.dragNeedsQueueOrder'), 'info');
        gesture.current = null;
        return false;
      }
      const block = blockFor(g.unit);
      // The second silent dead end, and it was there long before this gesture: a
      // row in NO band cannot be reordered at all, so the drag started, nothing
      // previewed, the drop did nothing and the list looked broken. A finished or
      // failed download has left the wait queue. Asked of the whole block now
      // rather than of the pressed row, because a marking that holds one settled
      // row and four queued ones is a perfectly good move.
      if (block.every((u) => unitIds(u).length === 0)) {
        toast(t('list.dragNotInQueue'), 'info');
        gesture.current = null;
        return false;
      }
      // Taken at this exact moment, before any preview has run for this move —
      // the one point at which the rendered order is guaranteed to still match
      // the server's own bandOrder.
      snapshotSlots();
      g.block = block;
      setRowDrag(block);
    }
    g.live = true;
    // From here the strip owns the pointer: every move and the release arrive
    // here whatever is painted underneath, which is what a list whose rows slide
    // out from under a still pointer needs and what the old stationary sheet
    // over the rows was faking.
    try {
      strip.setPointerCapture(g.pointerId);
    } catch {
      // The pointer is already gone (a window switch mid-press). The gesture
      // still works for as long as events keep arriving; the release ends it.
    }
    edgeScroll.current.box = scrollerOf(strip);
    if (!edgeScroll.current.frame) edgeScroll.current.frame = requestAnimationFrame(stepEdgeScroll);
    return true;
  }

  function applyGesture(g: Gesture): void {
    if (g.mode === 'select') sweepTo(g.atX, g.atY);
    else aimBlock(g.atY);
  }

  /**
   * Which row a point is on, live.
   *
   * elementFromPoint and not the frozen snapshot, and the two are not
   * interchangeable: nothing slides during a SWEEP, so the element under the
   * pointer is the row the eye sees, and reading it live is what keeps the sweep
   * honest while the list scrolls under it. A move is the opposite case and uses
   * the snapshot for exactly the opposite reason (see aimBlock).
   *
   * Off the rows - past either end of the list, or beside it in the margin - the
   * nearest row by Y, so sweeping downward past the last row keeps selecting to
   * the end instead of stopping at whatever the pointer last happened to cross.
   */
  function unitUnder(x: number, y: number): RowDragKey | null {
    const strip = stripRef.current;
    if (!strip) return null;
    const hit = document.elementFromPoint(x, y);
    const row = hit instanceof Element ? hit.closest<HTMLElement>('[data-row-key]') : null;
    if (row && strip.contains(row)) return unitOfRow(row);
    let best: HTMLElement | null = null;
    let bestDist = Infinity;
    strip.querySelectorAll<HTMLElement>('[data-task-id],[data-package-row]').forEach((el) => {
      const r = el.getBoundingClientRect();
      const d = y < r.top ? r.top - y : y > r.bottom ? y - r.bottom : 0;
      if (d < bestDist) {
        bestDist = d;
        best = el;
      }
    });
    return best ? unitOfRow(best) : null;
  }

  /**
   * The sweep: mark everything between where the press landed and where the
   * pointer is now.
   *
   * A RANGE FROM THE ANCHOR, not a trail of everywhere the pointer has been.
   * Sweeping down to row nine and back up to row three marks three to five, not
   * three to nine - which is Swing's own changeSelection(row, col, false, true)
   * and the only version of this that can be UNDONE by moving the mouse back.
   * It is also the same range a Shift-click makes, off the same anchor and
   * through the same rangeIds, so the two gestures cannot come to disagree.
   *
   * Only when the row under the pointer has actually changed. A selection write
   * re-renders every drawn row, and on a long list that is measured in hundreds
   * of milliseconds (see listRows.ts) - once per row crossed is the cost of the
   * gesture, once per pointer move would be the gesture being unusable.
   *
   * NEVER AGAINST THE DIRECTION THE HAND WENT, and that guard exists because of
   * something measured rather than imagined. Both pages that host this list grow
   * a toolbar row the moment anything is marked (Collector.tsx and Downloads.tsx
   * both render their selection strip behind `selected.size > 0`), so the very
   * press that marks the first row pushes the whole table DOWN - measured on the
   * collector at 1400x1100: the row strip's top goes from 485 to 525, a clean
   * 40px, which is more than one row. The pointer has not moved, but the row
   * beneath it has, and the next reading answers with the row ABOVE the one that
   * was pressed. Sweeping five pixels DOWNWARD then marked the folder above,
   * header and all.
   *
   * The rule that fixes it says something true on its own: a range that reaches
   * back past the row the hand pressed, in the direction the hand did not go, is
   * never what was meant. Below the press point the sweep can only reach down
   * from the pressed row, above it only up, and crossing back the other way is
   * allowed the moment the hand actually crosses - which is what keeps sweeping
   * down and then back up past the start working. Measured from the PRESSED ROW
   * and not from the range anchor, because a Shift-press lands somewhere the
   * anchor is not, and clamping a Shift-drag against the anchor would collapse
   * the range the moment the hand moved back toward it. With no layout jump
   * under it the clamp never fires at all: the row under the pointer is always
   * on the side the pointer travelled to.
   */
  function sweepTo(x: number, y: number): void {
    const g = gesture.current;
    if (!g || !selection) return;
    const unit = unitUnder(x, y);
    if (!unit) return;
    const under = selectableOrder.findIndex((u) =>
      unit.kind === 'task' ? u.kind === 'task' && u.key === unit.id : u.kind === 'package' && u.key === unit.name,
    );
    if (under < 0) return;
    const anchor = selectAnchor.current ?? under;
    const pressed = g.fromIndex < 0 ? under : g.fromIndex;
    const travel = y - g.fromY;
    const to = travel > 0 ? Math.max(under, pressed) : travel < 0 ? Math.min(under, pressed) : pressed;
    if (to === g.sweptTo) return;
    g.sweptTo = to;
    const range = rangeIds(anchor, to);
    // Ctrl held at the press means "and these as well", so the sweep adds to
    // what was marked before rather than replacing it.
    selection.set(g.mods.ctrlKey || g.mods.metaKey ? new Set([...g.before, ...range]) : range);
  }

  /**
   * The move's own hit test, run against the frozen snapshot and the pointer's
   * own Y - never against the element the browser delivered the event to.
   *
   * Once the preview starts sliding rows, the element under the pointer is
   * itself a consequence of the LAST answer this gave: under a stationary
   * pointer sitting on the boundary between two rows that is a closed loop, and
   * the symptom is rows endlessly swapping back and forth rather than settling.
   * Reading against a snapshot the preview never touches means "which row, which
   * half" is a pure function of the pointer's own position.
   */
  function aimBlock(clientY: number): void {
    const g = gesture.current;
    if (!g || g.block.length === 0) return;
    const aim = aimAt(rowSlotsRef.current, stripY(clientY), g.block, {
      // A link row stands for the folder it is in, which is what makes the whole
      // of an open folder a landing place for another folder instead of only its
      // 44px header - see aimAt. It is also how aimAt knows that a link of a
      // folder that is itself travelling is in flight too.
      packageOf: (id) => taskById.get(id)?.package ?? '',
      // A row in ANOTHER band is a legal target (dropBlock re-bands what lands
      // there), so it is offered as one. What is skipped is a unit in no band at
      // all: a finished or failed download the queue cannot be told to move, and
      // a folder whose links do not agree on one band.
      canTarget: (unit) => unitBand(unit) !== null,
    });
    if (!aim) return;
    g.over = aim;
    setDragOver((prev) => (prev && prev.after === aim.after && sameUnit(prev.target, aim.target) ? prev : aim));
  }

  // applyGesture through a ref, because the frame loop below re-schedules ITSELF
  // and would otherwise keep answering out of the render it was started in: a
  // websocket tick during a long drag rebuilds selectableOrder and the view, and
  // a scroll step reading last minute's copy of them would sweep against a list
  // that no longer exists. Every other path into applyGesture comes from an
  // event handler, which React rebuilds per render and which is therefore always
  // current.
  const applyRef = useRef(applyGesture);
  applyRef.current = applyGesture;

  /** The list crawls under the pointer while the pointer sits near an edge. */
  function stepEdgeScroll(): void {
    edgeScroll.current.frame = 0;
    const g = gesture.current;
    if (!g || !g.live) return;
    const box = edgeScroll.current.box;
    const r = box?.getBoundingClientRect();
    const above = g.atY - (r ? r.top : 0);
    const below = (r ? r.bottom : window.innerHeight) - g.atY;
    let dy = 0;
    if (above < EDGE_BAND_PX) dy = -Math.ceil(((EDGE_BAND_PX - Math.max(above, 0)) / EDGE_BAND_PX) * EDGE_SPEED_PX);
    else if (below < EDGE_BAND_PX) dy = Math.ceil(((EDGE_BAND_PX - Math.max(below, 0)) / EDGE_BAND_PX) * EDGE_SPEED_PX);
    if (dy !== 0) {
      if (box) box.scrollTop += dy;
      else window.scrollBy(0, dy);
      // The content moved under a pointer that did not, so the answer has to be
      // taken again - otherwise the list scrolls past the place it is pointing
      // at and the preview stands still through the whole scroll.
      applyRef.current(g);
    }
    edgeScroll.current.frame = requestAnimationFrame(stepEdgeScroll);
  }

  function stopEdgeScroll(): void {
    if (edgeScroll.current.frame) cancelAnimationFrame(edgeScroll.current.frame);
    edgeScroll.current = { frame: 0, box: null };
  }

  function moveGesture(e: PointerEvent<HTMLElement>): void {
    const g = gesture.current;
    if (!g || e.pointerId !== g.pointerId) return;
    g.atX = e.clientX;
    g.atY = e.clientY;
    if (!g.live) {
      if (!pastThreshold(e.clientX - g.fromX, e.clientY - g.fromY)) return;
      if (!beginGesture(g, e.currentTarget)) return;
    }
    applyGesture(g);
  }

  /** The release. `commit` is false for a gesture the browser or Escape took
   *  away rather than one somebody let go of. */
  function finishGesture(commit: boolean): void {
    const g = gesture.current;
    gesture.current = null;
    stopEdgeScroll();
    const strip = stripRef.current;
    if (g && strip?.hasPointerCapture(g.pointerId)) strip.releasePointerCapture(g.pointerId);
    setRowDrag(null);
    setDragOver(null);
    if (!g) return;
    if (!g.live) {
      // A press that never travelled. On a marked row that is the deferred
      // collapse - see (4) in the section head; on an unmarked one the press
      // itself already did the marking and there is nothing left to do.
      if (g.mode === 'move') selectFromUnit(g.unit, PLAIN);
      return;
    }
    if (!commit) {
      // Escape and a cancelled pointer put a sweep back where it started. A move
      // has changed nothing yet, so there is nothing to put back.
      if (g.mode === 'select') selection?.set(new Set(g.before));
      return;
    }
    if (g.mode === 'move' && g.over && g.block.length > 0) dropBlock(g.block, g.over.target, g.over.after);
  }

  function abortGesture(): void {
    if (gesture.current) finishGesture(false);
  }

  // Escape lets go of a gesture in flight, which is the one thing a person
  // holding a mouse button down has no other way to do: there is no "put it back
  // and forget it" in a press that is already halfway across the list.
  //
  // Through a ref, because the listener is installed once and the function it
  // calls is rebuilt on every render along with everything it reads. The same
  // ref is what tears a gesture down if the list unmounts mid-press.
  const abortRef = useRef(abortGesture);
  abortRef.current = abortGesture;
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') abortRef.current();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      abortRef.current();
    };
  }, []);

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
  // and since a package's ids stay contiguous, dragging a folder rearranged
  // nothing at all on screen ("Ich kann ordner nicht per drag and drop
  // verschieben", jdp). The preview is now built the way the real list is -
  // one flat run of tasks handed to groupByPackage - so a folder that moved
  // past another folder genuinely comes out in the new place. That is the
  // point of reusing groupByPackage rather than re-deriving a group order
  // here: the preview cannot promise an arrangement the committed order
  // would not produce, because both come out of the same function.
  //
  // ONE SPLICE, AND IT IS THE DROP'S OWN (rowDrag.ts's previewOrder): the block
  // is lifted out where it is and put back against the target's edge. It used
  // to be a second, band-shaped arrangement instead - the dragged BAND's slots
  // refilled in a new order, every other row left alone - and that is why a
  // drag across two priorities showed nothing at all while the pointer was
  // down: the old preview asked which band the target was in and gave up when
  // it was a different one, so the list stood still for the whole gesture.
  // Measured on a list of six folders at two priorities: every cross-priority
  // folder drag moved exactly nothing until the mouse was released. A splice
  // that never asks which band the target is in cannot have that hole - and it
  // is what lets a marking spanning two bands preview honestly now that
  // dropBlock will actually carry one.
  //
  // The block is several units, so the ids come out of all of them, in the order
  // the list draws them: what the preview shows gathering at the drop point is
  // exactly what dropBlock splices there.
  const liveView = useMemo(() => {
    if (!rowDrag || !dragOver) return view;
    const flat = view.flatMap(([, items]) => items);
    // The MOVABLE ids of each unit, which is what the drop moves too: a
    // finished link inside a folder is not in the wait queue, so it is not part
    // of the block that travels and the preview must not pretend it is.
    const next = previewOrder(
      flat.map((x) => x.id),
      rowDrag.flatMap(unitIds),
      unitIds(dragOver.target),
      dragOver.after,
    );
    if (!next) return view;
    const byId = new Map(flat.map((x) => [x.id, x] as const));
    const shuffled: Task[] = [];
    for (const id of next) {
      const t = byId.get(id);
      if (t) shuffled.push(t);
    }
    // One task per id or the two lists describe different things and there is
    // nothing honest to preview - a mismatch would drop a row out of the
    // preview, which React would then render as a row that vanished mid-drag.
    if (shuffled.length !== flat.length) return view;
    return groupByPackage(shuffled);
    // unitIds and unitBand read view and taskById, and taskById is derived from
    // view; listing them again would only re-run this on renders that cannot
    // change its result.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, rowDrag, dragOver]);

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
   * `wanted` is the previewed arrangement flattened to the rows that are
   * actually ON SCREEN - a folded folder contributes its header and none of its
   * links, and on a windowed list a row the window never drew contributes
   * nothing either, because it has no box to move. The stacking itself is
   * rowDrag.ts's stackOffsets, where it can be run on plain numbers.
   *
   * Bails out whole rather than in part. A poll that adds or removes a task
   * mid-drag leaves the snapshot describing a list that no longer exists, and
   * half-correct offsets would leave rows lying on top of each other; no
   * preview at all is the honest state, and the drop itself still commits
   * against dragOver, which never depended on this. That check is a count of
   * the rows the snapshot MEASURED against the rows the preview can place, so
   * it means the same thing on a windowed list as on a whole one: a task the
   * window drew and the preview cannot account for, or the other way round, is
   * still a snapshot that has gone stale.
   */
  function previewOffsets(): Map<string, number> {
    if (!rowDrag || !dragOver) return NO_OFFSETS;
    const slots = rowSlotsRef.current;
    const drawn = new Set(slots.map((s) => rowKey(s.unit)));
    const wanted: string[] = [];
    for (const [name, items] of liveView) {
      const header = rowKey({ kind: 'package', name });
      if (drawn.has(header)) wanted.push(header);
      if (!collapsed.has(name)) {
        for (const x of items) {
          const key = rowKey({ kind: 'task', id: x.id });
          if (drawn.has(key)) wanted.push(key);
        }
      }
    }
    return stackOffsets(slots, wanted, rowKey) ?? NO_OFFSETS;
  }

  const rowOffsets = previewOffsets();

  // The rows a move is carrying, as the keys the two row components ask about.
  // A folder's own links are in it as well as its header: they travel with it,
  // so they dim with it, even though nobody named them one by one.
  const movingRows = new Set<string>();
  if (rowDrag) {
    for (const u of rowDrag) {
      movingRows.add(rowKey(u));
      if (u.kind === 'package') for (const id of unitAllIds(u)) movingRows.add(rowKey({ kind: 'task', id }));
    }
  }

  const dnd: RowDnD = {
    moving: (unit) => movingRows.has(rowKey(unit)),
    press: pressRow,
    // Every row in the table gets one of these, including the ones that are not
    // moving: the transition has to already be on a row before its offset
    // changes, or the first step aside it makes is a jump. A row with nothing to
    // do simply carries translateY(0).
    //
    // The whole style DISAPPEARS the moment the move ends, which is what puts
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

  // Which slice of `rows` is worth drawing, and how much empty space stands in
  // for the rest. Whole list, no spacers, for anything short enough not to need
  // it (see VIRTUALIZE_ABOVE).
  const win = useRowWindow(rows, stripRef);

  // The list from the keyboard - see listKeyboard.ts for why a WINDOWED list
  // needs a file of its own for it. selectUnit goes in whole rather than being
  // reimplemented: Shift with an arrow has to extend from the same anchor a
  // Shift-click uses, or the two gestures disagree about what is selected.
  const keys = useListKeyboard({
    rows,
    win,
    stripRef,
    selectUnit,
    collapsed,
    collapse,
    expand,
    openProperties: () => {
      setPropertiesAutoFocus(true);
      setPropertiesOpen(true);
    },
    enabled: rows.length > 0,
  });

  // Scroll a named row into view, exactly once per request.
  //
  // TRAP, and it decides how this is written: `win` is a fresh object on every
  // scroll (useRowWindow's own listener sets viewport state per animation
  // frame). An effect that DEPENDED on it would scroll, which moves the
  // viewport, which makes a new `win`, which scrolls again - an unbreakable
  // loop on any list with a running download. So `win` is read through a ref
  // and is not a dependency at all, and a second ref holds the last revealKey
  // actually handled.
  //
  // That second ref is set only once the row has been FOUND. A revealKey can
  // arrive one commit before the task does (the page may have just dropped a
  // peer scope, and useTasks refills from a fresh socket snapshot), and marking
  // it handled on the way past would swallow the request for good.
  const winRef = useRef(win);
  winRef.current = win;
  const revealed = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!revealKey || revealed.current === revealKey) return;
    const strip = stripRef.current;
    if (!strip) return;
    const key = revealKey.split('#')[0];
    const index = rows.findIndex((r) => r.key === key);
    if (index < 0) return;
    revealed.current = revealKey;

    // Walked rather than matched with a selector: a package name is whatever
    // somebody typed, and quoting it into an attribute selector is a class of
    // bug nobody needs. At most a windowful of nodes is drawn.
    const find = () =>
      [...strip.querySelectorAll<HTMLElement>('[data-row-key]')].find(
        (el) => el.getAttribute('data-row-key') === key,
      ) ?? null;

    const here = find();
    if (here) {
      here.scrollIntoView({ block: 'center', inline: 'nearest' });
      return;
    }

    // The row is outside the drawn window, so there is no element to scroll to
    // at all - the two spacers carry no data attributes on purpose. A 1px
    // marker at the row's own offset stands in for it, and the scroll it causes
    // is what moves the window over the real row.
    const mark = document.createElement('div');
    mark.style.position = 'absolute';
    mark.style.top = `${winRef.current.topOf(index)}px`;
    mark.style.height = '1px';
    mark.style.width = '1px';
    strip.appendChild(mark);
    mark.scrollIntoView({ block: 'center', inline: 'nearest' });
    mark.remove();

    // Then again, precisely. The first pass landed on ROW_ESTIMATE for every
    // row nobody has ever drawn - measureRows only ever measures what is in the
    // window - and on a five-thousand-row list that is hundreds of pixels out.
    // Two frames: one for the render the scroll caused, one for the measuring
    // pass that follows it.
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => find()?.scrollIntoView({ block: 'center', inline: 'nearest' }));
    });
    return () => {
      cancelAnimationFrame(first);
      cancelAnimationFrame(second);
    };
  }, [revealKey, rows]);

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
        <div className="flex items-center gap-2 px-4 pt-4">
          {/* The bubble on this badge is the HEADER's, not the list's own
              "what this is" text. That one is gone (jdp, 2026-09-06: "die i
              infobubble im kartentitel entfernen. auch in der linklisten
              card") and its prop with it, so it cannot come back by accident.
              What sits here instead is the one explanation the table still
              needs and could not otherwise be found: right-click the header
              for the column menu, click a label to sort (jdp, same day, on
              where that one should live: "die können wir ja in den
              cardtitelbadge machen"). */}
          {/* ONE bubble here, not two. A second one used to sit beside the badge
              spelling out that the list works from the keyboard - Tab in, arrows
              between rows, space to pick, Enter for the properties. It was
              removed on jdp's word, in the download window and in the collector,
              which share this component: an explanation of the keys is not what
              somebody opening a download list is looking for, and every keystroke
              it named is already listed under Settings, Shortcuts, which is where
              a person goes to look them up. Two bubbles side by side also make
              the reader decide which one holds their answer before they can read
              either. */}
          <SectionTitle hint={t('columns.headerHint')}>{title}</SectionTitle>
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
                profile={profile}
                sort={sort}
                onSort={(id) => setSort(nextSort(storedSort, id))}
                onReorder={(id, target, after) =>
                  persist({ order: moveColumn(layout.order.map((c) => c.id), id, target, after) })
                }
                onResize={onResize}
                onResizeReset={resetWidth}
                onMenu={setMenuAt}
              />

              {/* `rows`, which is built from `view` and not from liveView, and
                  that is the point: the rows stay in the order the server last
                  gave for the whole of a drag, and each one is SLID to where the
                  drag would put it (previewOffsets above, applied by dnd.slide).
                  Reordering them here instead is what the preview used to do,
                  and it left the move with nothing to animate but an
                  after-the-fact measurement. It also kept the rainbow hues
                  walking a moving list, so colours shuffled under the pointer as
                  a side effect of a reorder nobody had committed yet - the
                  `index` each row carries counts the resting order.

                  The two boxes around the slice are the rest of the list, as
                  height and nothing else (see useRowWindow). They carry no data
                  attributes on purpose: the drag snapshot, the right-click and
                  the selection all look rows up by those, and a spacer is not a
                  row that any of them may find. */}
              {/* role="tree" and not "treegrid": nothing here navigates to a
                  cell - left and right are close and open - so cell roles would
                  describe an interaction this list does not have, and a treegrid
                  would force roles onto the header row, onto the h-10 tail
                  spacer below and onto the absolutely-positioned action strip
                  that owns no grid track at all. If per-cell navigation is ever
                  added, this comment is where to say so.

                  `relative` is for the two probes, the keyboard's and the event
                  list's. A jump to a row the window has not drawn has nothing to
                  focus, so a one-pixel box is placed at that row's own offset
                  and scrolled into view; the commit after the scroll finds the
                  real row (listKeyboard.ts, and the revealKey effect above).
                  Neither probe carries data attributes, for exactly the reason
                  the two spacers do not: measureRows averages every
                  [data-row-key] it finds into the height estimate, and a 1px row
                  in that average drags the scrollbar for the whole list toward
                  zero. Both row roots are already `relative`, so nothing else
                  re-anchors. */}
              <div
                ref={stripRef}
                className="relative"
                role="tree"
                aria-multiselectable="true"
                aria-label={title}
                tabIndex={keys.stripTabIndex}
                onFocus={keys.onStripFocus}
                // THE WHOLE GESTURE HANGS HERE, not on the rows: a press bubbles
                // up from whichever row it landed on, and from the moment it
                // becomes a gesture this element holds the pointer capture, so
                // every move and the release arrive here whatever is painted
                // underneath. The answer always comes from the pointer's own
                // position and never from the element the event arrived on.
                //
                // THIS IS WHERE THE STATIONARY SHEET USED TO BE, and it is worth
                // saying what it was for, because the fault it covered is real
                // and pointer capture is the only reason it is gone. Under the
                // native HTML5 drag every row was displaced by a transform, the
                // browser hit-tests a transformed element where it is PAINTED,
                // and it only reconsiders what a drag is over when the POINTER
                // moves. So the preview slid a row out from under a stationary
                // pointer and a release with no last twitch of the mouse arrived
                // on an element that had never been sent a dragover: Chromium
                // then fired dragend with NO DROP AT ALL and the gesture was
                // swallowed in silence. Measured on a list of six folders: every
                // folder released without moving the mouse again was lost that
                // way, and the identical drag with one pixel of movement before
                // the release landed. A full-size invisible sheet over the rows
                // was what stopped the target changing under a still pointer. A
                // captured pointer cannot have the fault at all - what is painted
                // under it is not part of the question any more - so the sheet
                // came out with the drag it was propping up.
                onPointerMove={moveGesture}
                onPointerUp={(e) => {
                  if (gesture.current?.pointerId === e.pointerId) finishGesture(true);
                }}
                // The browser taking the pointer away (a touch turning into a
                // scroll, a system gesture, a window switch) is not a drop.
                onPointerCancel={(e) => {
                  if (gesture.current?.pointerId === e.pointerId) abortGesture();
                }}
                onLostPointerCapture={(e) => {
                  if (gesture.current?.pointerId === e.pointerId) abortGesture();
                }}
                // No native drag may start inside this list any more. Nothing
                // here sets `draggable`, but an <img> is draggable by default and
                // the hoster icon in every row is one - a press that lands on it
                // and moves would hand the pointer to the browser mid-sweep.
                onDragStart={(e) => e.preventDefault()}
              >
                {keys.probeTop !== null && (
                  <div
                    ref={keys.probeRef}
                    aria-hidden
                    style={{ position: 'absolute', top: keys.probeTop, height: 1, width: 1 }}
                  />
                )}
                {win.padTop > 0 && <div aria-hidden style={{ height: win.padTop }} />}
                {rows.slice(win.start, win.end).map((row) =>
                  row.kind === 'package' ? (
                    <PackageRow
                      key={row.key}
                      name={row.name}
                      items={row.items}
                      base={base}
                      ctx={ctx}
                      columns={layout.visible}
                      selection={selection}
                      collapsed={row.collapsed}
                      divider={row.divider}
                      onToggleCollapsed={() => toggle(row.name)}
                      dnd={dnd}
                      level={row.level}
                      posinset={row.posinset}
                      setsize={row.setsize}
                      current={keys.currentKey === row.key}
                      onKeyDown={(e) => keys.onRowKeyDown(e, row.key)}
                      onOpenProperties={() => {
                        setPropertiesAutoFocus(false);
                        setPropertiesOpen(true);
                      }}
                    />
                  ) : (
                    <TaskRow
                      key={row.key}
                      task={row.task}
                      index={row.index}
                      base={base}
                      ctx={ctx}
                      columns={layout.visible}
                      selection={selection}
                      dnd={dnd}
                      level={row.level}
                      posinset={row.posinset}
                      setsize={row.setsize}
                      current={keys.currentKey === row.key}
                      onKeyDown={(e) => keys.onRowKeyDown(e, row.key)}
                      onOpenProperties={() => {
                        setPropertiesAutoFocus(false);
                        setPropertiesOpen(true);
                      }}
                    />
                  ),
                )}
                {win.padBottom > 0 && <div aria-hidden style={{ height: win.padBottom }} />}
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
        <TaskProperties
          key={[...chosenIds].join(',')}
          ids={[...chosenIds]}
          tasks={chosen}
          base={base}
          autoFocus={propertiesAutoFocus}
        />
      )}
      {/* The read-only detail, below the properties card and below the strip.
          BELOW is load-bearing: useRowWindow measures stripRef after every
          commit, so a card above it that grows when an error string arrives, or
          a <video> that reserves space when it loads metadata, repaints the
          whole windowed slice each time it does.

          One row only. A folder header double-click selects a whole package,
          which TaskProperties is built for and this is not: there is no honest
          single answer for "the error", "the next attempt" or "the file" across
          eleven links. chosenIds and chosen are deliberately different sets -
          chosenIds is the whole selection, chosen only the rows this view can
          see - so a quick filter hiding the selected row must not put a one-row
          panel up for a different row.

          No key, unlike TaskProperties above: that one snapshots its boxes at
          mount, this one re-renders live off the task object the WebSocket
          replaces on every tick, and a key derived from anything that moves
          would tear the player down once a second. */}
      {propertiesOpen && chosenIds?.size === 1 && chosen.length === 1 && (
        <TaskDetailPanel task={chosen[0]} base={base} hue={hue === undefined ? undefined : hue + 1} />
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
