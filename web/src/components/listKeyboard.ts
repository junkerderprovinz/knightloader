import { useCallback, useEffect, useLayoutEffect, useRef, useState, type FocusEvent, type KeyboardEvent, type RefObject } from 'react';
import type { ListRow, RowWindow } from './listRows';

// Keyboard navigation for the windowed download list. Only the rows near the
// viewport are in the DOM (listRows.ts), so the cursor is a row key rather
// than an index, a jump to an undrawn row scrolls a probe into view before
// focusing, and the strip holds the tab stop while the current row is off
// screen.
//
// Delete belongs to ListToolbar's useRemoval, the Menu key and Shift+F10 to
// ContextMenu, and Alt chords to the queue-move commands, so none of them are
// handled here.

/** The modifier fields TaskListCard's selectUnit reads off a click. */
type SelectMods = { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean };

// A plain arrow acts as a click, Shift+arrow as a Shift-click and Space as a
// Ctrl-click, so keys and pointer share one anchor and one range rule.
const REPLACE: SelectMods = { ctrlKey: false, metaKey: false, shiftKey: false };
const EXTEND: SelectMods = { ctrlKey: false, metaKey: false, shiftKey: true };
const PICK: SelectMods = { ctrlKey: true, metaKey: false, shiftKey: false };

export interface ListKeyboard {
  /**
   * The row holding the tab stop, resolved to a fallback when the cursor's row
   * is gone. Never null while the list has rows, so Tab always has a target.
   */
  currentKey: string | null;
  /** Moves the cursor to a clicked row, so Tab resumes there. */
  setCurrent: (key: string) => void;
  onRowKeyDown: (e: KeyboardEvent<HTMLElement>, key: string) => void;
  /** 0 exactly while the current row is not drawn. */
  stripTabIndex: number;
  onStripFocus: (e: FocusEvent<HTMLElement>) => void;
  /** Where to put the one-pixel scroll probe, or null. */
  probeTop: number | null;
  probeRef: RefObject<HTMLDivElement | null>;
}

export function useListKeyboard({
  rows,
  win,
  stripRef,
  selectUnit,
  collapsed,
  collapse,
  expand,
  openProperties,
  enabled = true,
}: {
  rows: ListRow[];
  win: RowWindow;
  /** The row strip, queried for rows and holding focus while the current row
   *  is off screen. */
  stripRef: RefObject<HTMLDivElement | null>;
  /**
   * TaskListCard's selectUnit, which owns the Shift-range anchor. It takes a
   * raw package name or task id, never a prefixed row key, or it silently
   * selects nothing.
   */
  selectUnit: (kind: 'task' | 'package', key: string, ids: string[], e: SelectMods) => void;
  /** The folded set and its writers, shared with the twisty and the menu. */
  collapsed: Set<string>;
  collapse: (names: string[]) => void;
  expand: (names: string[]) => void;
  /** Opens the properties panel without selecting; see Enter below. */
  openProperties: () => void;
  /** False for an empty list, which should not take the tab stop. */
  enabled?: boolean;
}): ListKeyboard {
  // A key, because `rows` is rebuilt on every task update and indexes shift.
  const [currentKey, setCurrentKey] = useState<string | null>(null);
  // The fallback position when the cursor's row disappears.
  const lastIndex = useRef(0);
  // A row to focus once it is in the DOM.
  const pendingFocus = useRef<string | null>(null);
  // Set only for a jump to a row the window has not drawn.
  const [probeTop, setProbeTop] = useState<number | null>(null);
  const probeRef = useRef<HTMLDivElement>(null);
  // True while the strip holds focus we parked there, as opposed to a Tab.
  const parked = useRef(false);

  const currentIndex = currentKey === null ? -1 : rows.findIndex((r) => r.key === currentKey);
  const resolved = currentIndex >= 0 ? currentIndex : Math.min(lastIndex.current, rows.length - 1);
  const currentDrawn = resolved >= win.start && resolved < win.end;

  function goTo(index: number, mods: SelectMods | null): void {
    if (rows.length === 0) return;
    const i = Math.max(0, Math.min(index, rows.length - 1));
    const row = rows[i];
    if (!row) return;
    setCurrentKey(row.key);
    lastIndex.current = i;
    if (mods) {
      if (row.kind === 'package') {
        selectUnit(
          'package',
          row.name,
          row.items.map((x) => x.id),
          mods,
        );
      } else {
        selectUnit('task', row.task.id, [row.task.id], mods);
      }
    }
    pendingFocus.current = row.key;
    // An undrawn row gets a probe at its offset first; the scroll brings it into
    // the window and a later commit focuses it.
    if (i < win.start || i >= win.end) setProbeTop(win.topOf(i));
  }

  // No dependency list: this reacts to the DOM after every commit. Both
  // branches are guarded, so it settles.
  useEffect(() => {
    if (currentIndex >= 0) lastIndex.current = currentIndex;
    const probe = probeRef.current;
    if (probeTop !== null && probe) {
      // scrollIntoView, because the scrolling ancestor differs by page and the
      // nearest overflow ancestor only scrolls sideways.
      probe.scrollIntoView({ block: 'nearest' });
      setProbeTop(null);
      return;
    }
    const key = pendingFocus.current;
    if (key === null) return;
    // Package names are free text and would break the selector unescaped.
    const el = stripRef.current?.querySelector<HTMLElement>(`[data-row-key="${CSS.escape(key)}"]`);
    // Still pending until the probe's scroll lands.
    if (!el) return;
    pendingFocus.current = null;
    el.focus({ preventScroll: true });
    // The probe's offset may be an estimate, so this settles on the real row.
    el.scrollIntoView({ block: 'nearest' });
  });

  // When a scroll unmounts the current row, the browser drops focus to <body>
  // and the next Tab would restart at the top of the document. The strip takes
  // focus instead, in the same commit, but only when focus is on <body>.
  const wasDrawn = useRef(currentDrawn);
  useLayoutEffect(() => {
    const before = wasDrawn.current;
    wasDrawn.current = currentDrawn;
    if (!enabled || before === currentDrawn || currentDrawn) return;
    const strip = stripRef.current;
    if (!strip) return;
    const active = document.activeElement;
    if (active !== null && active !== document.body) return;
    parked.current = true;
    strip.focus({ preventScroll: true });
  }, [currentDrawn, enabled, stripRef]);

  const setCurrent = useCallback((key: string) => setCurrentKey(key), []);

  function onStripFocus(e: FocusEvent<HTMLElement>): void {
    // Focus from rows inside bubbles here too.
    if (e.target !== e.currentTarget) return;
    if (parked.current) {
      // Parked by us after a scroll; jumping back would undo that scroll.
      parked.current = false;
      return;
    }
    if (!enabled) return;
    // A Tab from outside: focus the remembered row without selecting.
    goTo(resolved < 0 ? 0 : resolved, null);
  }

  function onRowKeyDown(e: KeyboardEvent<HTMLElement>, key: string): void {
    if (!enabled) return;
    // Keys pressed on a button inside the row belong to that button.
    if (e.target !== e.currentTarget) return;
    if (e.altKey) return;
    // Repeats are allowed so a held arrow keeps walking; a selection write on
    // 5000 rows costs about 2.4ms.
    const rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl';
    const forward = rtl ? 'ArrowLeft' : 'ArrowRight';
    const back = rtl ? 'ArrowRight' : 'ArrowLeft';

    // The pressed row, safer than the cursor after a re-sort mid-keypress.
    const at = rows.findIndex((r) => r.key === key);
    const index = at >= 0 ? at : resolved;
    const row = rows[index];
    if (!row) return;

    // preventDefault stops the page scrolling; stopPropagation keeps
    // CommandDispatcher from also firing a command rebound onto a bare key.
    const take = () => {
      e.preventDefault();
      e.stopPropagation();
    };
    // Ctrl or Cmd moves only the focus.
    const mods = e.shiftKey ? EXTEND : e.ctrlKey || e.metaKey ? null : REPLACE;

    switch (e.key) {
      case 'ArrowDown':
        take();
        // Clamped, never wrapped: on a long list a wrap loses the place.
        goTo(index + 1, mods);
        return;
      case 'ArrowUp':
        take();
        goTo(index - 1, mods);
        return;
      case 'Home':
        take();
        goTo(0, mods);
        return;
      case 'End':
        take();
        goTo(rows.length - 1, mods);
        return;
      case ' ':
      // Older engines report 'Spacebar'.
      // falls through
      case 'Spacebar':
        take();
        if (row.kind === 'package') {
          selectUnit(
            'package',
            row.name,
            row.items.map((x) => x.id),
            PICK,
          );
        } else {
          selectUnit('task', row.task.id, [row.task.id], PICK);
        }
        return;
      case 'Enter':
        take();
        // No selection first: a new selection closes the properties panel in
        // the same commit. The arrow that reached this row already selected it.
        openProperties();
        return;
    }

    if (e.key === forward) {
      take();
      // Opens a shut folder, or steps onto its first link.
      if (row.kind !== 'package') return;
      if (collapsed.has(row.name)) expand([row.name]);
      else if (row.items.length > 0) goTo(index + 1, null);
      return;
    }

    if (e.key === back) {
      take();
      // Shuts an open folder, or steps from a link to its folder's header.
      if (row.kind === 'package') {
        if (!collapsed.has(row.name)) collapse([row.name]);
        return;
      }
      for (let i = index - 1; i >= 0; i--) {
        if (rows[i].kind === 'package') {
          goTo(i, null);
          return;
        }
      }
    }
  }

  return {
    // Resolved, since before any click there is no stored key.
    currentKey: rows[resolved]?.key ?? null,
    setCurrent,
    onRowKeyDown,
    stripTabIndex: enabled && !currentDrawn ? 0 : -1,
    onStripFocus,
    probeTop,
    probeRef,
  };
}
