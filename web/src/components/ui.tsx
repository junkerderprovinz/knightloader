// Primitives of the KEEP design language. Everything is expressed through the
// shared tokens in index.css, so a sibling app inherits the look by adopting
// that file — see the comment block there.
import { useEffect } from 'react';
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from 'react';
import { IconClose } from '../lib/icons';

type ButtonKind = 'primary' | 'secondary' | 'ghost' | 'danger';

const kindClass: Record<ButtonKind, string> = {
  primary: 'bg-accent text-accentContrast hover:brightness-110',
  secondary: 'bg-carbon-surface2 text-carbon-text hover:bg-carbon-surface3',
  ghost: 'text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text',
  danger: 'text-statusFail hover:bg-statusFailBg',
};

export function Button({
  kind = 'primary',
  icon,
  children,
  className = '',
  ...rest
}: {
  kind?: ButtonKind;
  icon?: ReactNode;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  const iconOnly = icon && !children;
  return (
    <button
      className={`inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] text-sm font-medium
        transition duration-150 select-none disabled:opacity-35 disabled:pointer-events-none
        motion-safe:active:scale-[.98] ${iconOnly ? 'p-2' : 'px-3.5 py-2'} ${kindClass[kind]} ${className}`}
      {...rest}
    >
      {icon}
      {children}
    </button>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-xs text-carbon-textSub">{label}</span>
      {children}
      {hint && <span className="text-[11px] leading-snug text-carbon-textMuted">{hint}</span>}
    </label>
  );
}

const inputClass =
  'w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text ' +
  'placeholder:text-carbon-textMuted outline-none transition-shadow ' +
  'focus:shadow-[0_0_0_2px_var(--focus-ring)]';

export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={inputClass} {...props} />;
}

export function TextArea(props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={`${inputClass} resize-y`} {...props} />;
}

export function NumberInput({
  value,
  onValue,
  min,
  max,
  step = 1,
  ...rest
}: {
  value: number;
  onValue: (n: number) => void;
  min?: number;
  max?: number;
  step?: number;
} & Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange'>) {
  return (
    <input
      type="number"
      className={`${inputClass} keep-num`}
      value={value}
      min={min}
      max={max}
      step={step}
      onChange={(e) => onValue(Number(e.target.value))}
      {...rest}
    />
  );
}

export function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="flex items-center gap-3 text-left text-sm text-carbon-text select-none"
    >
      <span
        className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${
          checked ? 'bg-accent' : 'bg-carbon-surface3'
        }`}
      >
        {/* left-0 is load-bearing: without it the knob starts from its static
            position, which a button's inherited text-align centres — the knob
            then slides out past the pill. Tailwind v4 also animates the
            `translate` property here, not `transform`. */}
        <span
          className={`absolute left-0 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-[translate] duration-150 ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
      <span>{label}</span>
    </button>
  );
}

// Card is the one raised surface. Never nest it inside another Card.
export function Card({
  children,
  className = '',
  hover = false,
}: {
  children: ReactNode;
  className?: string;
  hover?: boolean;
}) {
  return (
    <div
      className={`keep-card p-5 ${
        hover ? 'transition-transform duration-150 motion-safe:hover:-translate-y-0.5' : ''
      } ${className}`}
    >
      {children}
    </div>
  );
}

// PageHeader is the single title block every page opens with.
export function PageHeader({
  title,
  subtitle,
  right,
}: {
  title: string;
  subtitle?: string;
  right?: ReactNode;
}) {
  return (
    <header className="flex items-end gap-4">
      <div className="min-w-0">
        <h1 className="text-[28px] font-semibold leading-tight tracking-tight text-carbon-text">{title}</h1>
        {subtitle && <p className="text-carbon-textMuted text-sm mt-1">{subtitle}</p>}
      </div>
      <span className="flex-1" />
      {right}
    </header>
  );
}

// One treatment for "there is nothing here", used everywhere so an empty app
// never looks broken — and, where it makes sense, offers the way out.
export function EmptyState({
  icon,
  title,
  hint,
  action,
}: {
  icon?: ReactNode;
  title: string;
  hint?: string;
  action?: ReactNode;
}) {
  return (
    <div className="keep-card flex flex-col items-center gap-2 p-10 text-center">
      {icon && <div className="text-carbon-textMuted/60">{icon}</div>}
      <div className="text-sm text-carbon-textSub">{title}</div>
      {hint && <div className="text-[11px] text-carbon-textMuted">{hint}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

// A quiet placeholder while a page's data is still on the wire.
export function LoadingCard({ label }: { label: string }) {
  return (
    <div className="keep-card p-10 text-center text-sm text-carbon-textMuted">{label}</div>
  );
}

// A fault state that says what went wrong and offers a way to recover.
export function ErrorCard({ message, retry, retryLabel }: { message: string; retry?: () => void; retryLabel?: string }) {
  return (
    <div className="keep-card flex flex-col items-center gap-3 p-10 text-center">
      <div className="text-sm text-statusFail">{message}</div>
      {retry && (
        <Button kind="secondary" onClick={retry}>
          {retryLabel}
        </Button>
      )}
    </div>
  );
}

// SectionTitle labels a group of content without adding another surface.
export function SectionTitle({ children, right }: { children: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex items-baseline gap-3">
      <h2 className="text-sm font-semibold text-carbon-textSub">{children}</h2>
      <span className="flex-1" />
      {right}
    </div>
  );
}

// Modal is the one overlay treatment: a dimmed page and a single raised panel.
// Escape and a click on the backdrop both close it, so it never traps anyone.
export function Modal({
  title,
  onClose,
  children,
  footer,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="keep-card w-full max-w-md p-5 flex flex-col gap-5" role="dialog" aria-modal="true">
        <div className="flex items-center gap-3">
          <h2 className="text-sm font-semibold text-carbon-text">{title}</h2>
          <span className="flex-1" />
          <Button kind="ghost" icon={<IconClose width={16} height={16} />} onClick={onClose} aria-label={title} />
        </div>
        {children}
        {footer && <div className="flex items-center gap-3">{footer}</div>}
      </div>
    </div>
  );
}
