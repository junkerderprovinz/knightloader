import { useRef, type InputHTMLAttributes, type KeyboardEvent, type ReactNode, type Ref } from 'react';
import { useT } from '../lib/i18n';
import { IconClose, IconSearch } from '../lib/icons';
import { InfoBubble, useTooltip } from './ui';

/**
 * SearchBox is the one search field, in the settings search's look wherever a
 * window or a list is searched. Focus is a step up the surface ramp and a lit
 * magnifier rather than a ring, because the settings bar sits at the top edge
 * of a scrolling column that cuts off anything drawn outside it, and one field
 * looks the same everywhere. Escape empties a field that holds text, and the
 * cross does the same. `end` holds controls that belong inside the field, such
 * as a list search's category; `hint` is the (i) at its end.
 */
export function SearchBox({
  value,
  onChange,
  label,
  hint,
  end,
  onKeyDown,
  inputRef,
  className = '',
  ...input
}: {
  value: string;
  onChange: (next: string) => void;
  /** The placeholder, which also names the field. */
  label: string;
  hint?: string;
  end?: ReactNode;
  inputRef?: Ref<HTMLInputElement>;
  className?: string;
} & Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange' | 'type' | 'placeholder' | 'className'>) {
  const { t } = useT();
  const own = useRef<HTMLInputElement | null>(null);
  // Passed as disabled while the button is absent, so the bubble closes when
  // the button leaves: an unmounted trigger fires no mouseleave.
  const clearTip = useTooltip<HTMLButtonElement>(t('search.clear'), !value);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...clearTipProps } = clearTip.triggerProps;

  function setRef(el: HTMLInputElement | null) {
    own.current = el;
    if (typeof inputRef === 'function') inputRef(el);
    else if (inputRef) inputRef.current = el;
  }

  function keyDown(e: KeyboardEvent<HTMLInputElement>) {
    onKeyDown?.(e);
    if (e.defaultPrevented || e.key !== 'Escape' || !value) return;
    // Escape empties rather than blurs, and does not also close the window.
    e.stopPropagation();
    e.preventDefault();
    onChange('');
  }

  return (
    <div
      data-new="search-box"
      className={`group flex h-[var(--btn-h)] items-center gap-1 rounded-[var(--radius-control)] bg-carbon-surface2 pe-1 ps-2.5
        transition-colors has-[input:focus]:bg-carbon-surface3 ${className}`}
    >
      <IconSearch
        className="shrink-0 text-carbon-textMuted transition-colors group-has-[input:focus]:text-accentGlyph"
        width={15}
        height={15}
      />
      <input
        {...input}
        ref={setRef}
        type="search"
        value={value}
        placeholder={label}
        aria-label={input['aria-label'] ?? label}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={keyDown}
        className="glim-focus-drawn h-full min-w-0 flex-1 bg-transparent text-sm text-carbon-text caret-accentGlyph
          placeholder:text-carbon-textMuted outline-none [&::-webkit-search-cancel-button]:hidden"
      />
      {value && (
        <button
          type="button"
          aria-label={t('search.clear')}
          {...clearTipProps}
          onClick={() => {
            onChange('');
            own.current?.focus();
          }}
          className="grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-pill)]
            text-carbon-textMuted transition-colors hover:bg-carbon-hoverRaised hover:text-carbon-text"
        >
          <IconClose width={13} height={13} />
        </button>
      )}
      {clearTip.node}
      {end}
      {hint && <InfoBubble tip={hint} className="me-1" />}
    </div>
  );
}
