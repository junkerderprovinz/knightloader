import { useEffect, useRef, useState } from 'react';
import { useT } from '../lib/i18n';

// The sidebar language picker: the current language with its flag, opening a
// list upward (it lives at the bottom of the rail). Flags come from the
// flag-icons stylesheet as `fi fi-XX`.
export function LanguagePicker({ className }: { className?: string }) {
  const { t, lang, setLang, languages } = useT();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const current = languages.find((l) => l.code === lang) ?? languages[0];

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
  }, [open]);

  if (!current) return null;

  return (
    <div className="relative" ref={ref}>
      <button
        aria-label={`${t('lang.label')}: ${current.label}`}
        title={`${t('lang.label')}: ${current.label}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={className}
      >
        <Flag code={current.flag} />
        <span className="flex-1 text-left">{current.label}</span>
      </button>

      {open && (
        <div
          role="listbox"
          aria-label={t('lang.label')}
          className="glim-card absolute bottom-full left-0 z-50 mb-2 max-h-72 w-52 overflow-y-auto p-1"
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
