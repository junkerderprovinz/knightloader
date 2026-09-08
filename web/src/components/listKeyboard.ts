import { useCallback, useEffect, useLayoutEffect, useRef, useState, type FocusEvent, type KeyboardEvent, type RefObject } from 'react';
import type { ListRow, RowWindow } from './listRows';

/**
 * The download list, from the keyboard.
 *
 * WHY THIS IS ITS OWN FILE AND NOT TWENTY LINES IN TaskList.tsx: the list is
 * WINDOWED. Only the slice around the viewport is in the DOM (see
 * listRows.ts's useRowWindow), so "move the focus to row 3000" is not a call to
 * `.focus()` - that element does not exist yet, and the row the focus is
 * standing on right now stops existing the moment somebody spins a wheel. Every
 * awkward-looking thing below is one half of that single problem:
 *
 *   - the cursor is a row KEY, never an index,
 *   - a jump to an undrawn row scrolls a one-pixel probe first and focuses on
 *     the commit after,
 *   - and the strip itself takes the tab stop back whenever the current row is
 *     not on screen, so the list never falls out of the tab order.
 *
 * WHAT THIS FILE DELIBERATELY DOES NOT OWN:
 *
 *   Delete and Shift+Delete. ListToolbar.tsx's own useRemoval already binds
 *   them on window, guarded against typing, and a focused row is a div rather
 *   than an input so that guard passes it straight through. A second listener
 *   on the same key would only race the first - lib/commands/downloads.ts says
 *   the same thing at its own Del.
 *
 *   The Menu key and Shift+F10. ContextMenu.tsx's anchorFromEvent already
 *   handles both, and ListToolbar.tsx resolves what was hit off
 *   `data-task-id` / `data-package-row`, which every row already carries. The
 *   moment a row is focusable the row menu opens from the keyboard with no
 *   code at all. Nothing to write; worth knowing before somebody writes it.
 *
 *   Alt with anything. lib/commands/downloads.ts binds alt+up / alt+down /
 *   alt+home / alt+end to "move this in the queue", and those have to keep
 *   working while a row has the focus - so an Alt chord is handed straight
 *   back to the dispatcher rather than eaten here.
 */

/** The three fields TaskListCard's own selectUnit reads off a click. */
type SelectMods = { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean };

// The three gestures the keys borrow from the mouse, spelled out once. A plain
// arrow is a plain click, Shift with an arrow is a Shift-click, and Space is a
// Ctrl-click - so the anchor moves, or does not move, in exactly the cases it
// already does for a pointer. There is no second anchor and no second range
// rule anywhere in this file, deliberately: two implementations of "what is
// selected" disagree the first time somebody mixes the gestures.
const REPLACE: SelectMods = { ctrlKey: false, metaKey: false, shiftKey: false };
const EXTEND: SelectMods = { ctrlKey: false, metaKey: false, shiftKey: true };
const PICK: SelectMods = { ctrlKey: true, metaKey: false, shiftKey: false };

export interface ListKeyboard {
  /**
   * Which row owns the tab stop, as a row key, and already RESOLVED: the row
   * the cursor is on, or the one it falls back to when that row is gone. Never
   * null while the list has rows in it - exactly one row is the tab stop from
   * the first render, or a Tab into the list would find nothing to land on.
   */
  currentKey: string | null;
  /** A click puts the cursor where the pointer went, so a later Tab resumes
   *  from the last row touched rather than from the top. */
  setCurrent: (key: string) => void;
  /** The row's own onKeyDown. It only ever acts on its own element - see the
   *  target guard for why. */
  onRowKeyDown: (e: KeyboardEvent<HTMLElement>, key: string) => void;
  /** 0 exactly while the current row is NOT drawn, so the list keeps a tab
   *  stop across a scroll that unmounted it. */
  stripTabIndex: number;
  onStripFocus: (e: FocusEvent<HTMLElement>) => void;
  /** Where to put the one-pixel scroll probe, or null when none is wanted. */
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
  /** The row strip, which is both what gets queried for a row element and what
   *  parks the focus when the current row is scrolled out of the DOM. */
  stripRef: RefObject<HTMLDivElement | null>;
  /**
   * TaskListCard's own selectUnit, passed in rather than reimplemented: the
   * anchor a Shift-range measures from lives in there, and a Shift+arrow that
   * grew its own anchor would disagree with a Shift-click about what is
   * selected the first time somebody used both.
   *
   * It takes the RAW identity - a package NAME or a task id - and never the
   * prefixed row key: it looks the unit up in selectableOrder, whose entries
   * are keyed the raw way, so a `pkg:` or `task:` prefix makes its findIndex
   * miss and the call return in silence. That failure looks exactly like
   * "the arrow keys move but select nothing", with no error anywhere.
   */
  selectUnit: (kind: 'task' | 'package', key: string, ids: string[], e: SelectMods) => void;
  /** The folded set and its two writers, shared with the twisty and the
   *  right-click menu, so the three can never disagree about what is open. */
  collapsed: Set<string>;
  collapse: (names: string[]) => void;
  expand: (names: string[]) => void;
  /** Opens the properties panel, and NOTHING else - see Enter below. */
  openProperties: () => void;
  /** False for a list with no rows in it: an empty table that takes the tab
   *  stop is a stop that leads nowhere. */
  enabled?: boolean;
}): ListKeyboard {
  // The cursor, held as a row KEY and never as an index. `rows` is rebuilt
  // whenever a task changes, which on a downloading list is about once a
  // second, and a finished link sorted out of the running half moves every
  // index below it. A number would quietly slide onto a different row while
  // nobody touched anything.
  const [currentKey, setCurrentKey] = useState<string | null>(null);
  // Where the cursor was the last time it resolved. A row can vanish outright
  // (a clean-up, a package folded over it), and this is where it falls back to
  // rather than to the top of the list.
  const lastIndex = useRef(0);
  // A row we intend to focus as soon as it is in the DOM. Cleared by the
  // post-commit effect the moment it finds it.
  const pendingFocus = useRef<string | null>(null);
  // Set only for a jump to a row the window has not drawn - see the probe.
  const [probeTop, setProbeTop] = useState<number | null>(null);
  const probeRef = useRef<HTMLDivElement>(null);
  // True while the focus sitting on the strip is focus WE put there, not a Tab
  // that just arrived from outside.
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
    // Inside the window the element is already there and the effect below will
    // find it on the very next commit. Outside it there is nothing to focus
    // yet, so a probe is placed at the row's own offset first and the scroll it
    // causes brings the row into the window; the commit after that focuses it.
    if (i < win.start || i >= win.end) setProbeTop(win.topOf(i));
  }

  // Runs after EVERY commit, with no dependency list, for the same reason
  // useRowWindow's own layout effect does: what it reacts to is the state of
  // the DOM, not the value of anything React can compare. Both branches are
  // guarded, so this settles rather than looping.
  useEffect(() => {
    if (currentIndex >= 0) lastIndex.current = currentIndex;
    const probe = probeRef.current;
    if (probeTop !== null && probe) {
      // scrollIntoView and never a hand-rolled walk up the ancestors: this
      // component genuinely cannot know which box scrolls (the page's own
      // <main> for downloads, the collector's own wrapper, and Layout.tsx
      // switches <main> between the two by route), and the NEAREST overflow
      // ancestor of the strip is the table's `overflow-x-auto`, which is
      // scrollable and scrolls only sideways - a "nearest scrollable ancestor"
      // walk finds it, writes scrollTop, and nothing moves.
      probe.scrollIntoView({ block: 'nearest' });
      setProbeTop(null);
      return;
    }
    const key = pendingFocus.current;
    if (key === null) return;
    // CSS.escape, because a package name is free user text and lands in the key
    // as `pkg:<name>`: one apostrophe or bracket in a folder name would throw a
    // SyntaxError out of here and take the render down with it.
    const el = stripRef.current?.querySelector<HTMLElement>(`[data-row-key="${CSS.escape(key)}"]`);
    // Not there yet: the scroll the probe asked for has not landed. Left
    // pending, and the commit that follows the scroll picks it up.
    if (!el) return;
    pendingFocus.current = null;
    el.focus({ preventScroll: true });
    // Not redundant after the probe: a probe placed from `topOf` on rows nobody
    // has measured is an estimate (see RowWindow.topOf), so it lands near the
    // row rather than on it. This is the line that converges it.
    el.scrollIntoView({ block: 'nearest' });
  });

  // THE ONE THAT KILLS THE FEATURE, and its fix.
  //
  // A wheel scroll of two screens unmounts whatever row the keyboard was
  // standing on. The browser then drops focus onto <body>, and the next Tab
  // restarts at the top of the DOCUMENT - the person's place in the page is
  // simply gone, and nothing on screen says so. So the moment the current row
  // stops being drawn, the strip takes the focus itself (it is already the tab
  // stop by then; see stripTabIndex) and holds the place until the cursor comes
  // back.
  //
  // A layout effect, not a passive one: it has to run in the same commit that
  // removed the row, before the browser paints and long before the next key.
  //
  // Only ever from <body>. If the focus is anywhere real - a search box, a
  // toolbar button, a badge inside a row still on screen - somebody put it
  // there on purpose and this must not take it away from them.
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
    // A focus event bubbles, so every row and every badge inside one arrives
    // here too. Only the strip's own focus is this handler's business.
    if (e.target !== e.currentTarget) return;
    if (parked.current) {
      // We put it here ourselves, one scroll ago. Jumping back to the row now
      // would undo the very scroll the person just made.
      parked.current = false;
      return;
    }
    if (!enabled) return;
    // A real Tab, arriving from outside the list: bring the remembered row back
    // into view and hand it the focus. No selection call - Tab is not a click.
    goTo(resolved < 0 ? 0 : resolved, null);
  }

  function onRowKeyDown(e: KeyboardEvent<HTMLElement>, key: string): void {
    if (!enabled) return;
    // Key events bubble. A press while the focus is on a row's Pause badge or
    // its twisty - both real buttons - would otherwise move the row cursor
    // while the browser also activates the button, and Space would do both at
    // once. This handler acts on its own element and nothing else.
    if (e.target !== e.currentTarget) return;
    // Alt belongs to the queue-move commands - see this file's own header.
    if (e.altKey) return;
    // No `if (e.repeat) return` here, unlike CommandDispatcher: a held arrow
    // key has to keep walking. It is affordable because a selection write on
    // 5000 rows costs about 2.4ms and nothing downstream of the selection
    // fetches - which stops being true the day one of the row menus grows a
    // per-selection request.
    const rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl';
    const forward = rtl ? 'ArrowLeft' : 'ArrowRight';
    const back = rtl ? 'ArrowRight' : 'ArrowLeft';

    // The row that was actually pressed, which after a re-sort mid-keypress is
    // a safer answer than the cursor's own resolved index.
    const at = rows.findIndex((r) => r.key === key);
    const index = at >= 0 ? at : resolved;
    const row = rows[index];
    if (!row) return;

    // Every handled key stops here twice over: preventDefault because a row is
    // a div and nothing suppresses the page-scrolling defaults of Space, the
    // arrows and Home/End for us, and stopPropagation because CommandDispatcher
    // listens on window and its only guard is "is this an input" - which a row
    // is not. Nothing collides today (every list command is bound with Alt),
    // but Settings, Shortcuts lets anyone rebind a command onto a bare
    // ArrowDown, and then one press would both walk the list and fire it.
    const take = () => {
      e.preventDefault();
      e.stopPropagation();
    };
    // A plain arrow replaces the selection and moves the anchor, Shift extends
    // from the anchor without moving it, Ctrl/Cmd moves nothing but the focus.
    const mods = e.shiftKey ? EXTEND : e.ctrlKey || e.metaKey ? null : REPLACE;

    switch (e.key) {
      case 'ArrowDown':
        take();
        // Clamped at both ends and never wrapped. ContextMenu's own step()
        // wraps because a menu is short and cyclic; a list of five thousand
        // rows that jumps from the last row to the first on one keypress is a
        // lost place, not a convenience. Please do not "fix" this into a wrap.
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
      // 'Spacebar' is what older engines report; both mean the same press.
      // falls through
      case 'Spacebar':
        take();
        // Picks this row out or puts it back, exactly as a Ctrl-click does,
        // and does not move: the cursor is already here.
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
        // Opens the panel and does NOT select first. TaskListCard closes the
        // properties panel on every new selection identity, and selectUnit
        // always hands `set()` a fresh Set - so an Enter that selected before
        // opening would open the panel and close it in the same commit, and
        // the bug would read as Enter doing nothing at all. The arrow that got
        // you to this row already did the selecting.
        openProperties();
        return;
    }

    if (e.key === forward) {
      take();
      // Into the folder: open it if it is shut, and otherwise step onto the
      // first link inside it. On a link there is nothing further in, so
      // nothing happens - the tree is two levels deep and this is the bottom.
      if (row.kind !== 'package') return;
      if (collapsed.has(row.name)) expand([row.name]);
      else if (row.items.length > 0) goTo(index + 1, null);
      return;
    }

    if (e.key === back) {
      take();
      // Out of the folder: shut it if it is open, and from a link step out to
      // the header of the folder it sits in. A shut folder is already as far
      // out as this list goes.
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
    // The RESOLVED key and not the stored one: before anything has been
    // clicked or walked to there is no stored key at all, and a list where no
    // row carries tabIndex 0 is a list Tab cannot get into.
    currentKey: rows[resolved]?.key ?? null,
    setCurrent,
    onRowKeyDown,
    stripTabIndex: enabled && !currentDrawn ? 0 : -1,
    onStripFocus,
    probeTop,
    probeRef,
  };
}
