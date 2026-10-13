import { useEffect, useImperativeHandle, useRef, useState, type ComponentProps, type FocusEvent, type KeyboardEvent, type Ref } from 'react';
import { useT } from '../lib/i18n';
import { IconSearch } from '../lib/icons';
import { SearchBox } from './SearchBox';
import { IconBadge } from './ui';

/** What a shortcut can do with a search that may still be its button. */
export interface SearchHandle {
  /** Turns the button into the field, if it is not one yet, and puts the caret in it. */
  open: () => void;
}

/** Whether focus went somewhere that still belongs to the field, such as the menu of its category picker. */
function stillInside(box: HTMLElement | null, to: EventTarget | null): boolean {
  if (!(to instanceof Node)) return false;
  if (box?.contains(to)) return true;
  return to instanceof Element && to.closest('[role="menu"]') !== null;
}

/**
 * SearchToggle is the search on a page: the Search button, which turns into
 * the shared field (SearchBox) when pressed, with the caret in it. Escape or
 * leaving the field while it is empty turns it back into the button. A field
 * that holds text stays, so a forgotten search cannot hide behind a button.
 */
export function SearchToggle({
  value,
  onChange,
  handle,
  labelled = true,
  hue,
  onKeyDown,
  ...box
}: ComponentProps<typeof SearchBox> & {
  handle?: Ref<SearchHandle>;
  /** The button with its name beside the glyph, as the label setting allows. */
  labelled?: boolean;
  hue?: number;
}) {
  const { t } = useT();
  const [opened, setOpened] = useState(false);
  // Bumped on every request for the caret, which the field takes once drawn.
  const [asked, setAsked] = useState(0);
  const [backToButton, setBackToButton] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const wrap = useRef<HTMLDivElement>(null);
  const shown = opened || value !== '';

  const open = () => {
    setOpened(true);
    setAsked((n) => n + 1);
  };
  useImperativeHandle(handle, () => ({ open }), []);

  useEffect(() => {
    if (asked > 0) input.current?.focus();
  }, [asked]);

  // The button keeps the keyboard where Escape left the field. IconBadge's
  // own ref belongs to its tooltip, so the button is found in the wrapper.
  useEffect(() => {
    if (shown || !backToButton) return;
    setBackToButton(false);
    wrap.current?.querySelector('button')?.focus();
  }, [shown, backToButton]);

  function keyDown(e: KeyboardEvent<HTMLInputElement>) {
    onKeyDown?.(e);
    if (e.defaultPrevented || e.key !== 'Escape' || value) return;
    // An empty field closes before the window around it does.
    e.stopPropagation();
    e.preventDefault();
    setOpened(false);
    setBackToButton(true);
  }

  function blur(e: FocusEvent<HTMLDivElement>) {
    if (value === '' && !stillInside(wrap.current, e.relatedTarget)) setOpened(false);
  }

  if (!shown) {
    return (
      <div ref={wrap} data-new="search-button" className="shrink-0">
        <IconBadge
          labelled={labelled}
          hue={hue}
          icon={<IconSearch width={16} height={16} />}
          // The button's own name rather than the field's placeholder;
          // web/check-placeholder-as-label.mjs keeps it so.
          title={t('search.toggle')}
          aria-label={t('search.toggle')}
          aria-expanded={false}
          onClick={open}
        />
      </div>
    );
  }
  return (
    <div ref={wrap} className="contents" onBlur={blur}>
      <SearchBox {...box} value={value} onChange={onChange} inputRef={input} onKeyDown={keyDown} />
    </div>
  );
}
