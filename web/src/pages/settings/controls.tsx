import {
  useEffect,
  useState,
  type ComponentProps,
  type CSSProperties,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react';
import { useNavigate } from 'react-router-dom';
import { PathInput } from '../../components/FolderPicker';
import { Button, HintBubble, Modal, TextArea } from '../../components/ui';
import { hueVars } from '../../lib/appearance';
import { IconClose } from '../../lib/icons';
import { useT } from '../../lib/i18n';
import { useFieldError } from './context';
import { usePendingJump } from './jump';

/**
 * useWholePage tells whether a settings tile draws the whole page it belongs
 * to. A page that stands in the sidebar is set up there, so its tile keeps
 * only the switches and the way to the page and nothing shows twice. The
 * settings search still lands on any of the page's rows: a result that leads
 * here brings the page along until the tile is left.
 */
export function useWholePage(page: string, pinned: boolean): boolean {
  const jump = usePendingJump();
  const asked = jump?.page === page;
  const [found, setFound] = useState(asked);
  useEffect(() => {
    if (asked) setFound(true);
  }, [asked]);
  return !pinned || asked || found;
}

/**
 * OpenPageRow leads from a pinned page's settings tile to the page itself. Its
 * button says only "Open", since the row already names the page.
 */
export function OpenPageRow({ label, to }: { label: string; to: string }) {
  const { t } = useT();
  const navigate = useNavigate();
  return (
    <div className="flex items-center justify-between gap-4">
      {/* data-glim-label is where the settings search scrolls to, as on ToggleRow. */}
      <span data-glim-label={label} className="text-sm text-carbon-text">
        {label}
      </span>
      <Button kind="secondary" onClick={() => navigate(to)}>
        {t('settings.pinned.open')}
      </Button>
    </div>
  );
}

type Hint = ComponentProps<typeof HintBubble>['hint'];

/**
 * SettingRow is one setting on a line: its name at the start, with the
 * explanation behind an (i), and its control at the end. A selector goes in as
 * a direct child, since it measures the row to know when it has to wrap; where
 * the row is too narrow for both, the control moves under the name and keeps
 * the row's end.
 */
export function SettingRow({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: Hint;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
      {/* data-glim-label is where the settings search scrolls to, as on ToggleRow. */}
      <span data-glim-label={label} className="flex min-w-0 flex-[1_1_12.5rem] items-center gap-1.5 text-sm text-carbon-text">
        {label}
        <HintBubble hint={hint} />
      </span>
      {children}
    </div>
  );
}

/**
 * SubHead is the small heading of a group inside a card, with its explanation
 * behind an (i) and room for an action at its end. A card has one notch badge,
 * so a second group in it is named by this instead.
 */
export function SubHead({ label, hint, right }: { label: string; hint?: Hint; right?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
      {/* data-glim-label is where the settings search scrolls to. */}
      <h3 data-glim-label={label} className="glim-eyebrow flex items-center gap-1.5 font-semibold">
        {label}
        <HintBubble hint={hint} />
      </h3>
      {right && <span className="ms-auto flex flex-wrap items-center justify-end gap-2">{right}</span>}
    </div>
  );
}

/**
 * SubCard groups rows inside a card under a small heading: flat, one step up
 * the surface ramp, at the card radius (`.glim-subcard` in index.css). It sets
 * a group apart without hiding it behind a fold.
 */
export function SubCard({
  label,
  hint,
  right,
  className = 'gap-4',
  children,
}: {
  label: string;
  hint?: Hint;
  /** An action at the heading's end, such as the button that adds a row. */
  right?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section className={`glim-subcard flex flex-col px-4 pb-4 pt-3 ${className}`}>
      <SubHead label={label} hint={hint} right={right} />
      {children}
    </section>
  );
}

/**
 * Sheet is the window a list row opens in place of unfolding: the row's
 * settings over the page, with Close as its one answer, since every field in
 * it saves itself.
 */
export function Sheet({
  title,
  hint,
  hue,
  wide = false,
  onClose,
  children,
}: {
  title: string;
  hint?: string;
  hue?: number;
  wide?: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  const { t } = useT();
  return (
    <Modal
      title={title}
      hint={hint}
      hue={hue}
      height="capped"
      wide={wide}
      onClose={onClose}
      footer={<Button kind="ghost" labelled icon={<IconClose />} title={t('common.close')} onClick={onClose} />}
    >
      {children}
    </Modal>
  );
}

/**
 * SettingPathInput is PathInput for a folder setting, with the server's
 * refusal of what it holds beneath it.
 */
export function SettingPathInput({
  field,
  ...props
}: Omit<ComponentProps<typeof PathInput>, 'error'> & { field: string }) {
  const error = useFieldError(field);
  return <PathInput {...props} error={error} />;
}

/**
 * ListArea edits a list setting as a box with one entry per line. Blank lines
 * go out as they are and the server drops them: taken out here, the new line
 * an Enter starts would vanish before anything could be typed on it.
 */
export function ListArea({
  lines,
  onLines,
  ...props
}: Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'value' | 'onChange'> & {
  lines?: string[] | null;
  onLines: (lines: string[]) => void;
}) {
  return (
    <TextArea
      spellCheck={false}
      {...props}
      value={(lines ?? []).join('\n')}
      onChange={(e) => onLines(e.target.value === '' ? [] : e.target.value.split('\n'))}
    />
  );
}

/**
 * RowRefusal is the server's refusal of one row of a settings list, such as
 * "connections.2". It goes under the row's line, where the list shows it while
 * the row's window is closed.
 */
export function RowRefusal({
  field,
  explained = false,
  className = '',
}: {
  field: string;
  /** The row already says why in its own words, so the refusal is not repeated. */
  explained?: boolean;
  className?: string;
}) {
  const text = useFieldError(field);
  if (!text || explained) return null;
  return <p className={`pb-2.5 text-xs text-statusWarn ${className}`}>{text}</p>;
}

/**
 * NeutralSwitch is the switch for columns of switches, such as the module list
 * and the advanced key table. It matches Toggle in role, geometry and hue, so
 * the two read as one control.
 */
export function NeutralSwitch({
  on,
  onChange,
  name,
  onLabel,
  offLabel,
  disabled = false,
  hue,
}: {
  on: boolean;
  onChange: (next: boolean) => void;
  /** The accessible name; the visible label is the on/off word beside it. */
  name: string;
  /** The on/off word beside the pill; both empty renders the pill alone. */
  onLabel?: string;
  offLabel?: string;
  disabled?: boolean;
  /** Position among the switches of one card, 0-based like Toggle's; omit for a lone switch. */
  hue?: number;
}) {
  const worded = Boolean(onLabel || offLabel);
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={name}
      disabled={disabled}
      onClick={() => onChange(!on)}
      className={`${hue !== undefined ? 'glim-hue' : ''} flex shrink-0 items-center gap-2.5 text-start text-xs text-carbon-textSub select-none disabled:opacity-40`}
      style={hue !== undefined ? (hueVars(hue) as CSSProperties) : undefined}
    >
      {worded && <span className="glim-num w-6 text-end">{on ? onLabel : offLabel}</span>}
      <span
        className={`relative h-5 w-9 shrink-0 rounded-[var(--radius-pill)] transition-colors ${
          on ? (hue !== undefined ? 'bg-accent' : 'bg-carbon-textMuted') : 'bg-carbon-surface3'
        }`}
      >
        {/* Without start-0 the knob starts from the button's centred text
            position and slides past the pill. Tailwind v4 animates the
            `translate` property, not `transform`. */}
        <span
          className={`absolute start-0 top-0.5 h-4 w-4 rounded-[var(--radius-pill)] bg-carbon-background shadow-sm transition-[translate] duration-150 ${
            on ? 'translate-x-4 rtl:-translate-x-4' : 'translate-x-0.5 rtl:-translate-x-0.5'
          }`}
        />
      </span>
    </button>
  );
}
