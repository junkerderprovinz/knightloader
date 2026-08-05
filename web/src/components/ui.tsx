// Small form/control primitives styled with the Carbon tokens, so the app needs
// no component framework. All are theme-aware via the carbon-* / accent tokens.
import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from 'react';

type ButtonKind = 'primary' | 'secondary' | 'ghost' | 'danger';

const kindClass: Record<ButtonKind, string> = {
  primary: 'bg-accent text-accentContrast hover:brightness-95',
  secondary: 'bg-carbon-surface2 text-carbon-text hover:bg-carbon-surface3',
  ghost: 'bg-transparent text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text',
  danger: 'bg-transparent text-statusFail hover:bg-statusFailBg',
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
      className={`inline-flex items-center justify-center gap-2 rounded-lg font-medium text-sm transition
        duration-150 select-none disabled:opacity-40 disabled:pointer-events-none
        motion-safe:active:scale-[.97] ${iconOnly ? 'p-2' : 'px-3.5 py-2'} ${kindClass[kind]} ${className}`}
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
      {hint && <span className="text-[11px] text-carbon-textMuted">{hint}</span>}
    </label>
  );
}

const inputClass =
  'w-full rounded-lg bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text placeholder:text-carbon-textMuted ' +
  'outline-none focus:ring-2 focus:ring-[var(--status-info-solid)]';

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
      className={inputClass}
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
      className="flex items-center gap-3 text-sm text-carbon-text select-none"
    >
      <span
        className={`relative h-5 w-9 rounded-full transition-colors ${
          checked ? 'bg-accent' : 'bg-carbon-surface3'
        }`}
      >
        <span
          className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
      <span>{label}</span>
    </button>
  );
}

// Card is the standard surface panel: soft elevation + hairline (.kl-card).
// `hover` adds a subtle lift for interactive cards.
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
      className={`kl-card p-5 ${hover ? 'transition-transform duration-150 motion-safe:hover:-translate-y-0.5' : ''} ${className}`}
    >
      {children}
    </div>
  );
}

// PageHeader is the consistent title block at the top of every page.
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
        <h1 className="text-[26px] font-bold leading-tight text-carbon-text">{title}</h1>
        {subtitle && <p className="text-carbon-textMuted text-sm mt-1">{subtitle}</p>}
      </div>
      <span className="flex-1" />
      {right}
    </header>
  );
}
