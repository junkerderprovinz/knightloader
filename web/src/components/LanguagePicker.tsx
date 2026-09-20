import { useEffect, useRef, useState } from 'react';
import { useT } from '../lib/i18n';
import { setLangPickerOpen, toggleLangPickerOpen, useLangPickerOpen } from '../lib/langPickerOpen';

// The language picker. Its open state lives in lib/langPickerOpen.ts so the
// command palette can open the sidebar's instance.
export function LanguagePicker({
  className,
  standalone,
  direction = 'up',
}: {
  className?: string;
  /**
   * Keeps the open state local, for a second instance mounted alongside the
   * sidebar's. A shared flag would let the sidebar's outside-click handler
   * close this one before an option's click lands.
   */
  standalone?: boolean;
  /** 'up' in the sidebar at the bottom of the screen, 'down' higher up a page. */
  direction?: 'up' | 'down';
}) {
  const { t, lang, setLang, languages } = useT();
  const shared = useLangPickerOpen();
  const [localOpen, setLocalOpen] = useState(false);
  const open = standalone ? localOpen : shared;
  const setOpen = standalone ? setLocalOpen : setLangPickerOpen;
  const toggleOpen = standalone ? () => setLocalOpen((v) => !v) : toggleLangPickerOpen;
  const ref = useRef<HTMLDivElement>(null);
  const current = languages.find((l) => l.code === lang) ?? languages[0];

  // Loaded lazily so half a megabyte of flags stays out of the first paint.
  useEffect(() => {
    void import('../flags.css');
  }, []);

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('mousedown', onClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onClick);
      document.removeEventListener('keydown', onKey);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  if (!current) return null;

  return (
    <div className="relative" ref={ref}>
      <button
        aria-label={`${t('lang.label')}: ${current.label}`}
        title={`${t('lang.label')}: ${current.label}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={toggleOpen}
        className={className}
      >
        <Flag code={current.flag} />
        <span className="flex-1 text-left">{current.label}</span>
      </button>

      {open && (
        <div
          role="listbox"
          aria-label={t('lang.label')}
          className={`glim-card absolute left-0 z-50 max-h-72 w-52 overflow-y-auto p-1 ${
            direction === 'up' ? 'bottom-full mb-2' : 'top-full mt-2'
          }`}
        >
          {languages.map((l) => (
            <button
              key={l.code}
              role="option"
              aria-selected={l.code === lang}
              onClick={() => {
                setLang(l.code);
                setOpen(false);
              }}
              className={`flex w-full items-center gap-2.5 rounded-[var(--radius-control)] px-2.5 py-1.5 text-left text-sm transition-colors ${
                l.code === lang
                  ? 'bg-carbon-surface2 text-carbon-text'
                  : 'text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text'
              }`}
            >
              <Flag code={l.flag} />
              <span>{l.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function Flag({ code }: { code: string }) {
  return (
    <span
      className={`fi fi-${code} shrink-0 rounded-[2px]`}
      style={{ width: '1.25em', height: '1em', display: 'inline-block' }}
      aria-hidden
    />
  );
}
