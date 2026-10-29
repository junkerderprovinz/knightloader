import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react';
import {
  closeWindow,
  drawsOwnFrame,
  isWindowMaximised,
  minimiseWindow,
  startWindowDrag,
  toggleMaximiseWindow,
  useEdgeResize,
} from '../lib/desktop';
import { useT } from '../lib/i18n';
import { IconClose, IconWindowMaximise, IconWindowMinimise, IconWindowRestore } from '../lib/icons';
import { useTooltip } from './ui';

/**
 * WindowBar is the desktop app's title bar where the main window has no
 * native one: a strip to drag and the window buttons. The sidebar already
 * shows the logo and the name. It is fixed over the top of the window, and
 * index.css moves the page and every scrim below it through --window-bar.
 */
export function WindowBar() {
  return drawsOwnFrame() ? <Bar /> : null;
}

/** How long the page has to stop resizing before the window is asked whether it is maximised. */
const SETTLE_MS = 100;

/**
 * useMaximised follows whether the window is maximised. Maximising and
 * restoring resize the page whatever started them, this bar, a snap or the
 * keyboard, so a resize that has settled is the cue to ask.
 */
function useMaximised(): boolean {
  const [maximised, setMaximised] = useState(false);
  useEffect(() => {
    let timer: number | undefined;
    const ask = () => void isWindowMaximised().then(setMaximised, () => {});
    const settle = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(ask, SETTLE_MS);
    };
    ask();
    window.addEventListener('resize', settle);
    return () => {
      window.removeEventListener('resize', settle);
      window.clearTimeout(timer);
    };
  }, []);
  return maximised;
}

const onButton = (e: MouseEvent) => (e.target as Element).closest('button') !== null;

function Bar() {
  const { t } = useT();
  const maximised = useMaximised();
  useEdgeResize(!maximised);
  // A press on the bar moves the window once the pointer moves, so a click or
  // a double click stays a click. An edge the resize has taken is left to it.
  const pressed = useRef(false);
  const down = (e: MouseEvent) => {
    pressed.current = e.button === 0 && !e.defaultPrevented && !onButton(e);
  };
  const move = (e: MouseEvent) => {
    if (!pressed.current) return;
    pressed.current = false;
    if (e.buttons === 1) startWindowDrag();
  };
  const toggle = (e: MouseEvent) => {
    if (!onButton(e)) void toggleMaximiseWindow().catch(() => {});
  };
  return (
    // Named, so a screen reader still says which app the buttons belong to.
    <header
      aria-label="KnightLoader"
      className="glim-window-bar"
      onMouseDown={down}
      onMouseMove={move}
      onMouseUp={() => (pressed.current = false)}
      onDoubleClick={toggle}
    >
      <div className="ms-auto flex h-full shrink-0">
        <WindowButton label={t('window.minimise')} icon={<IconWindowMinimise />} onPress={minimiseWindow} />
        <WindowButton
          label={maximised ? t('window.restore') : t('window.maximise')}
          icon={maximised ? <IconWindowRestore /> : <IconWindowMaximise />}
          onPress={toggleMaximiseWindow}
        />
        <WindowButton label={t('window.close')} icon={<IconClose />} onPress={closeWindow} />
      </div>
    </header>
  );
}

/**
 * WindowButton takes the bar's full height, so a pointer thrown into the
 * corner of a maximised window still lands on it, and shows the house square
 * inside. Closing hovers like its neighbours: it hides to the tray or quits
 * the way the native button does, and nothing is lost that a confirmation
 * would guard.
 */
function WindowButton({ label, icon, onPress }: { label: string; icon: ReactNode; onPress: () => Promise<void> }) {
  const tip = useTooltip<HTMLButtonElement>(label);
  // A <button> brings its own role and tab stop.
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-label={label}
        className="glim-window-button glim-focus-drawn group"
        {...hover}
        onClick={() => void onPress().catch(() => {})}
      >
        <span className="glim-btn-icon glim-window-square motion-safe:group-active:scale-[.97]">
          <span className="glim-btn-glyph">{icon}</span>
        </span>
      </button>
      {tip.node}
    </>
  );
}
