import {
  type ComponentProps,
  type CSSProperties,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react';
import { PathInput } from '../../components/FolderPicker';
import { Button, HintBubble, Modal, RowLabel, TextArea } from '../../components/ui';
import { hueVars } from '../../lib/appearance';
import { IconClose } from '../../lib/icons';
import { useT } from '../../lib/i18n';
import { useFieldError } from './context';

type Hint = ComponentProps<typeof HintBubble>['hint'];

/** How wide a SettingRow's one field is: a number's width, or a line of text's, which a dropdown takes too. */
export type RowControl = 'short' | 'long';

// Each width gives way where the row has less room, down to what still sits
// beside the name's 12.5rem, so a dialog or a phone keeps name and field on one
// line. Below that the field takes a line of its own at the row's width.
const CONTROL_WIDTH: Record<RowControl, string> = {
  short: 'w-full @min-[19rem]:w-[min(10rem,calc(100cqw-13.5rem))]',
  long: 'w-full @min-[24rem]:w-[min(20rem,calc(100cqw-13.5rem))]',
};

/**
 * SettingRow is one setting on a line: its name at the start, with the
 * explanation behind an (i), and its control at the end. A selector goes in as
 * a direct child, since it measures the row to know when it has to wrap; where
 * the row is too narrow for both, the control moves under the name and keeps
 * the row's end.
 *
 * `control` is for one dropdown or input field: the row becomes its <label>,
 * so the name is the field's name too, and the field takes a width that fits
 * beside the name. Every dropdown and input in the app stands this way
 * (check-setting-rows.mjs); a list or a text area keeps its name above it.
 * `compound` is for a field with buttons of its own: it gets the same width,
 * but no <label>, which would hand a click on any of them to the field.
 */
export function SettingRow({
  label,
  hint,
  control,
  compound = false,
  children,
}: {
  label: string;
  hint?: Hint;
  control?: RowControl;
  compound?: boolean;
  children: ReactNode;
}) {
  const Row = control && !compound ? 'label' : 'div';
  return (
    <Row className={`flex flex-wrap items-center justify-end gap-x-4 gap-y-2 ${control ? '@container w-full' : ''}`}>
      {/* data-glim-label is where the settings search scrolls to, as on ToggleRow. */}
      <span data-glim-label={label} className="min-w-0 flex-[1_1_12.5rem] text-sm text-carbon-text">
        <RowLabel label={label} hint={hint} />
      </span>
      {control ? <span className={CONTROL_WIDTH[control]}>{children}</span> : children}
    </Row>
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
      <h3 data-glim-label={label} className="glim-eyebrow flex items-center gap-1.5 font-semibold leading-4">
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
  widest = false,
  onClose,
  children,
}: {
  title: string;
  hint?: string;
  hue?: number;
  wide?: boolean;
  /** For a row whose selector, such as the priority ladder, has to stay on one line. */
  widest?: boolean;
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
      widest={widest}
      onClose={onClose}
      // Filled, since it is the window's one button and a bare word would not read as one.
      footer={<Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={onClose} />}
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
