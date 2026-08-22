import { useEffect, useRef, useState } from 'react';
import { useT } from '../lib/i18n';
import { setLangPickerOpen, toggleLangPickerOpen, useLangPickerOpen } from '../lib/langPickerOpen';

// The sidebar language picker: the current language with its flag, opening a
// list upward (it lives at the bottom of the rail). Flags come from the
// flag-icons stylesheet as `fi fi-XX`.
//
// `open` is lib/langPickerOpen.ts's module-level store rather than a local
// useState: the command palette's "open the language picker" / "close the
// language picker" commands (lib/commands/language.ts) drive the same
// dropdown this component's own button does, and the two never meet in the
// tree otherwise.
//
// `standalone` opts a SECOND, simultaneously-mounted instance (today, only
// OnboardingWizard.tsx's welcome step) out of that shared store and into its
// own local useState instead. The shared store is a singleton by design -
// "the sidebar's own instance" above is not a loose description, it is the
// one thing lib/langPickerOpen.ts's `open` boolean was ever meant to track -
// and the wizard mounts its own, separately-styled copy of this component
// WHILE the sidebar's copy stays mounted underneath the modal. Both reading
// one shared flag meant opening either one opened both, each with its own
// outside-click listener scoped to its own ref; clicking an option inside
// the wizard's dropdown landed a mousedown OUTSIDE the sidebar instance's
// ref, so the sidebar's listener fired setLangPickerOpen(false) first,
// unmounting the wizard's own dropdown before the click event that would
// have called setLang() could still land on it - a language choice that
// silently did nothing. `standalone` gives the wizard's instance a state
// nothing else can reach into, which is what two independent dropdowns
// actually need; the sidebar's own usage is unchanged.
export function LanguagePicker({
  className,
  standalone,
  direction = 'up',
}: {
  className?: string;
  standalone?: boolean;
  /** Which way the option list opens. The sidebar sits at the bottom of the
   * screen so it always opens 'up'; a copy placed further up the page (the
   * Aussehen settings tab) needs 'down' or the list would open off-screen. */
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

  // The flag sheet is fetched here rather than imported at the app root, so
  // half a megabyte of coats of arms is not in the way of the first paint. It
  // lands a moment later and the flags appear; until then the labels carry the
  // menu on their own, which is why the label is never just a flag.
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
