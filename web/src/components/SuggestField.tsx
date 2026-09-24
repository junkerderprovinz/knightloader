// A text field that offers the values already in use, in the app's own menu.
// A <datalist> would do the same with the browser's list, a white panel no
// theme here reaches (GlimStone rule 18).
import { useRef, useState, type KeyboardEvent } from 'react';
import { ContextMenu, anchorBelow, useContextMenu } from './ContextMenu';
import { TextInput } from './ui';
import { IconChevronDown } from '../lib/icons';

/** How many suggestions the menu shows; typing narrows the rest down. */
const MAX_SHOWN = 50;

/**
 * SuggestField is free text with suggestions. Typing, a click in the field or
 * ArrowDown opens the menu under it, narrowed to the suggestions containing
 * what was typed. ArrowDown moves into the menu and Enter there takes one;
 * Enter in the field is `onEnter`.
 */
export function SuggestField({
  value,
  onChange,
  suggestions,
  label,
  onEnter,
  autoFocus,
}: {
  value: string;
  onChange: (value: string) => void;
  suggestions: string[];
  /** What the field holds, which names its menu. */
  label: string;
  onEnter?: () => void;
  autoFocus?: boolean;
}) {
  const menu = useContextMenu();
  const field = useRef<HTMLInputElement>(null);
  // A prefilled value is a guess somebody else made, so it narrows nothing
  // until the field is edited.
  const [typed, setTyped] = useState(false);
  // A new key remounts the menu, which is how ArrowDown takes the focus into a
  // menu that opened with the focus left in the field.
  const [entry, setEntry] = useState({ n: 0, hold: true });
  const wasOpen = useRef(false);

  const needle = typed ? value.trim().toLowerCase() : '';
  const shown = suggestions.filter((s) => s.toLowerCase().includes(needle)).slice(0, MAX_SHOWN);
  // The menu stays asked for while nothing matches, so widening the text
  // brings it back, but it is only open while it has a row to show.
  const expanded = menu.anchor !== null && shown.length > 0;

  function open(hold: boolean) {
    setEntry((e) => ({ n: e.n + 1, hold }));
    menu.openAt(anchorBelow(field.current));
  }

  // Pointerdown, since the menu closes itself on the mousedown after it and
  // the click that follows must not open it again.
  function notePointer() {
    wasOpen.current = menu.anchor !== null;
  }

  function toggle() {
    if (!wasOpen.current) open(true);
    wasOpen.current = false;
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown' && shown.length > 0) {
      e.preventDefault();
      open(false);
    } else if (e.key === 'Escape' && expanded) {
      // Closes the menu and leaves the window around the field open.
      e.stopPropagation();
      menu.close();
    } else if (e.key === 'Enter') {
      menu.close();
      onEnter?.();
    }
  }

  return (
    <span className="relative block">
      <TextInput
        ref={field}
        autoFocus={autoFocus}
        value={value}
        aria-haspopup="menu"
        aria-expanded={expanded}
        className={suggestions.length > 0 ? 'pe-8' : ''}
        onChange={(e) => {
          onChange(e.target.value);
          setTyped(true);
          if (!menu.anchor) open(true);
        }}
        onBlur={(e) => {
          // Focus moving into the menu keeps it open; anywhere else closes it.
          const to = e.relatedTarget as Element | null;
          if (!to?.closest('[role="menu"]')) menu.close();
        }}
        onPointerDown={notePointer}
        onClick={toggle}
        onKeyDown={onKeyDown}
      />
      {/* Says there is a list to pick from, as a dropdown's chevron does. The
          keyboard reaches the list with ArrowDown, so this is no tab stop. */}
      {suggestions.length > 0 && (
        <button
          type="button"
          tabIndex={-1}
          aria-hidden
          // Keeps the focus in the field, where the typing goes.
          onMouseDown={(e) => e.preventDefault()}
          onPointerDown={notePointer}
          onClick={() => {
            field.current?.focus();
            toggle();
          }}
          className="absolute inset-y-0 end-0 flex w-[var(--btn-h)] items-center justify-center text-carbon-textMuted
            transition-colors hover:text-carbon-text"
        >
          <IconChevronDown width={14} height={14} />
        </button>
      )}
      {menu.anchor && (
        <ContextMenu
          key={entry.n}
          anchor={menu.anchor}
          label={label}
          minWidth={field.current?.offsetWidth}
          holdFocus={entry.hold}
          onClose={menu.close}
          groups={[
            {
              id: 'suggestions',
              items: shown.map((s) => ({
                id: s,
                label: s,
                checked: s === value.trim(),
                onSelect: () => {
                  onChange(s);
                  setTyped(false);
                },
              })),
            },
          ]}
        />
      )}
    </span>
  );
}
