// The app's one dropdown: a field-shaped trigger that opens the app's own menu
// with a check mark on the value in force. Never a native <select>, whose open
// list is the operating system's widget, a white panel with its own highlight
// that no theme here reaches (GlimStone rule 18).
import { useEffect, useRef, type KeyboardEvent, type MouseEvent } from 'react';
import { ContextMenu, anchorBelow, useContextMenu, type MenuItem } from './ContextMenu';
import { FIELD_TRIGGER, useTooltip } from './ui';
import { IconChevronDown } from '../lib/icons';
import { useShake } from '../lib/useShake';

export interface DropdownOption<T extends string = string> {
  value: T;
  label: string;
}

/** An entry that opens a list of its own, for a menu too long to show flat. */
export interface DropdownSubmenu<T extends string = string> {
  label: string;
  options: DropdownOption<T>[];
}

export type DropdownEntry<T extends string = string> = DropdownOption<T> | DropdownSubmenu<T>;

/**
 * `field` is a text field's box and type. `dense` is the same box in small
 * type, for a picker on a list row or beside a compact field. `inset` sits
 * inside another field, the search box, and is smaller than one.
 */
export type DropdownLook = 'field' | 'dense' | 'inset';

/**
 * `fill` takes the line like a text field. `widest` is as wide as the longest
 * option, as a native select is, so choosing never moves what stands beside
 * it. `value` fits the value on show, for a list cell too narrow for the
 * longest option.
 */
export type DropdownWidth = 'fill' | 'widest' | 'value';

const LOOK: Record<DropdownLook, string> = {
  field: `${FIELD_TRIGGER} items-center gap-2 ps-3 pe-2.5 text-start text-sm`,
  dense: `${FIELD_TRIGGER} items-center gap-1 ps-2 pe-1.5 text-start text-xs`,
  // bg-carbon-surface3/70 on the search box's surface2, and the hover one step
  // further, as the clear button beside it does.
  inset: `h-6 cursor-pointer items-center gap-1 rounded-[var(--radius-control)]
    bg-carbon-surface3/70 ps-2 pe-1.5 text-start text-xs text-carbon-textSub outline-none transition-colors
    hover:bg-carbon-surface3 hover:text-carbon-text focus-visible:shadow-[0_0_0_2px_var(--focus-ring)]`,
};

const WIDTH: Record<DropdownWidth, string> = {
  fill: 'flex w-full min-w-0',
  widest: 'inline-flex shrink-0',
  value: 'inline-flex shrink-0',
};

function isSubmenu<T extends string>(e: DropdownEntry<T>): e is DropdownSubmenu<T> {
  return 'options' in e;
}

/**
 * Dropdown picks one of `options`. The wheel steps through them while the
 * pointer rests on the trigger, clamped at both ends (rule 14): a real
 * listener with `{ passive: false }`, since React registers onWheel passive and
 * the page would scroll away under the pointer while the value changes.
 */
export function Dropdown<T extends string>({
  value,
  options,
  groups,
  onChange,
  label,
  look = 'field',
  width = look === 'field' ? 'fill' : 'widest',
  disabled,
  busy = false,
  tip,
  wheel = true,
  shake = 0,
  className = '',
}: {
  value: T;
  /** Every choice in menu order, which is also the order the wheel steps. */
  options: DropdownOption<T>[];
  /** The menu's rows in runs split by a hairline, where `options` in one run is not enough. */
  groups?: DropdownEntry<T>[][];
  onChange: (value: T) => void;
  /** What the dropdown chooses: the accessible name, with the value after it. */
  label: string;
  look?: DropdownLook;
  /** Defaults to `fill` for a field and to `widest` otherwise. */
  width?: DropdownWidth;
  disabled?: boolean;
  /**
   * A choice is out and its answer not back yet. The trigger takes no input
   * meanwhile, but unlike `disabled` it keeps the focus the menu has just
   * handed back to it.
   */
  busy?: boolean;
  /**
   * A hover bubble for a trigger with no caption beside it, where the value it
   * shows does not say what it picks.
   */
  tip?: string;
  /**
   * Whether the wheel steps through the options. A choice that acts on the
   * server, such as suspending every schedule, must not be made by scrolling
   * the page past it.
   */
  wheel?: boolean;
  /**
   * The caller's failure counter. Each bump shakes the trigger once, since a
   * value shown before the server refused it only snaps back otherwise.
   */
  shake?: number;
  className?: string;
}) {
  const menu = useContextMenu();
  const trigger = useRef<HTMLButtonElement | null>(null);
  const shakeRef = useShake<HTMLButtonElement>(shake);
  // Whether the menu was open when the press began: the menu closes itself on
  // that press, and the click that follows must not open it again.
  const wasOpen = useRef(false);
  const bubble = useTooltip<HTMLButtonElement>(tip);
  const { role: _tipRole, tabIndex: _tipTabIndex, ref: tipRef, ...tipHover } = bubble.triggerProps;

  const menuGroups = groups ?? [options];
  const all = menuGroups.flat().flatMap((e) => (isSubmenu(e) ? e.options : [e]));
  const text = all.find((o) => o.value === value)?.label ?? value;

  // Read through a ref, so the listener is attached once per trigger node.
  const live = useRef({ value, options, onChange, inert: disabled || busy });
  useEffect(() => {
    live.current = { value, options, onChange, inert: disabled || busy };
  });
  useEffect(() => {
    const el = trigger.current;
    if (!el || !wheel) return;
    const onWheel = (e: WheelEvent) => {
      const s = live.current;
      // Only the sign of deltaY counts: a trackpad reports fractions, and a
      // sideways flick says nothing about this control.
      if (s.inert || s.options.length < 2 || e.deltaY === 0) return;
      e.preventDefault();
      const at = s.options.findIndex((o) => o.value === s.value);
      // A value the list does not carry steps onto the first option.
      const next = at < 0 ? 0 : Math.min(s.options.length - 1, Math.max(0, at + (e.deltaY > 0 ? 1 : -1)));
      if (next !== at) s.onChange(s.options[next].value);
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, [wheel]);

  function open(el: HTMLElement) {
    menu.openAt(anchorBelow(el));
  }

  function onKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (busy) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      open(e.currentTarget);
    }
  }

  const choice = (o: DropdownOption<T>): MenuItem => ({
    id: `o-${o.value}`,
    label: o.label,
    checked: o.value === value,
    onSelect: () => {
      if (o.value !== value) onChange(o.value);
    },
  });

  // Every label in one grid cell, only the chosen one visible, so the cell is
  // as wide as the longest and the trigger does not jump when the value does.
  const labels = width === 'widest' ? [...new Set([text, ...all.map((o) => o.label)])] : [text];

  return (
    <>
      <button
        ref={(el) => {
          trigger.current = el;
          tipRef.current = el;
          shakeRef.current = el;
        }}
        type="button"
        disabled={disabled}
        aria-disabled={busy || undefined}
        aria-haspopup="menu"
        aria-expanded={menu.anchor !== null}
        aria-label={`${label}: ${text}`}
        {...(tip ? tipHover : undefined)}
        // Pointerdown, since the menu closes itself on the mousedown after it.
        onPointerDown={() => {
          wasOpen.current = menu.anchor !== null;
        }}
        onClick={(e: MouseEvent<HTMLButtonElement>) => {
          // A dropdown in a list row must not also select the row.
          e.stopPropagation();
          if (busy) return;
          const closing = wasOpen.current && e.detail > 0;
          wasOpen.current = false;
          if (!closing) open(e.currentTarget);
        }}
        onKeyDown={onKeyDown}
        className={`${WIDTH[width]} ${LOOK[look]} ${busy ? 'opacity-40' : ''} ${className}`}
      >
        <span className="grid min-w-0 flex-1">
          {labels.map((l) => (
            <span
              key={l}
              aria-hidden={l !== text}
              className={`col-start-1 row-start-1 truncate ${l === text ? '' : 'invisible'}`}
            >
              {l}
            </span>
          ))}
        </span>
        <IconChevronDown
          width={look === 'field' ? 14 : 12}
          height={look === 'field' ? 14 : 12}
          className="shrink-0 text-carbon-textMuted"
        />
      </button>
      {tip && bubble.node}
      {menu.anchor && (
        <ContextMenu
          anchor={menu.anchor}
          label={label}
          minWidth={trigger.current?.offsetWidth}
          onClose={menu.close}
          groups={menuGroups.map((group, g) => ({
            id: `g-${g}`,
            items: group.map((e) =>
              isSubmenu(e)
                ? { id: `s-${e.label}`, label: e.label, submenu: [{ id: e.label, items: e.options.map(choice) }] }
                : choice(e),
            ),
          }))}
        />
      )}
    </>
  );
}
