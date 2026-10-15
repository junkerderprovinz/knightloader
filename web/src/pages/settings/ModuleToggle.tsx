import { useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { Card, linkBadgeClass, SectionTitle, ToggleRow, useTooltip } from '../../components/ui';
import type { TranslationKey } from '../../lib/i18n';
import { IconForward } from '../../lib/icons';
import { useLabelMode } from '../../lib/labelModes';
import { useShake } from '../../lib/useShake';
import { useToast } from '../../lib/toast';
import { useFeatures } from './context';
import type { Feature } from './features';
import { clearJump, requestJump } from './jump';
import { pageGlyph } from './pageIcons';
import { SETTINGS_INDEX } from './searchIndex';
import { label, moduleDetail, moduleReason, switchRefusal, useTx } from './tx';

/**
 * How a module's card stands. A module that is off folds to its switch, so it
 * stays on its page and in the search; one that was never set up folds the same
 * way and says what is missing. Without a switch, or set up but not working,
 * the card stays open, since what is in it is how to fix that.
 */
type Fold = 'open' | 'off' | 'unset';

function foldOf(m: Feature | undefined): Fold {
  if (!m || m.verdict !== 'shipped' || m.switch === 'none' || m.enabled) return 'open';
  if (m.switch === 'parked' && !m.parked) return m.unset ? 'unset' : 'open';
  return 'off';
}

interface ModuleFold {
  m: Feature | undefined;
  fold: Fold;
  /** A module never set up, switched on here and waiting for its first value. */
  settingUp: boolean;
  setSettingUp: (on: boolean) => void;
  open: boolean;
}

function useModuleFold(id: string): ModuleFold {
  const { features } = useFeatures();
  const [settingUp, setSettingUp] = useState(false);
  const m = features.modules.find((f) => f.id === id);
  const fold = foldOf(m);
  const up = fold === 'unset' && settingUp;
  return { m, fold, settingUp: up, setSettingUp, open: fold === 'open' || up };
}

interface ToggleProps {
  id: string;
  /** A palette position of its own, for a switch among sibling switches; alone, it takes its card's colour. */
  hue?: number;
  /** What the module does, the first paragraph of the switch's (i). */
  hint?: string;
  /** A state only the page knows, such as an edit that is not saved yet. */
  blocked?: boolean;
}

/**
 * ModuleToggle is a module's switch on its page, the one place it is switched.
 * It goes through the module registry rather than the draft, and is left out
 * when the server has no such module. Off, a line under it says what that
 * means, and `children`, the module's own fields, are folded away.
 */
export function ModuleToggle(props: ToggleProps & { children?: ReactNode }) {
  const own = useModuleFold(props.id);
  const { children, ...rest } = props;
  return (
    <SwitchRow {...rest} f={own} folds={children !== undefined}>
      {own.open && children}
    </SwitchRow>
  );
}

function SwitchRow({
  id,
  hue,
  hint,
  blocked = false,
  f,
  folds,
  children,
}: ToggleProps & { f: ModuleFold; folds: boolean; children?: ReactNode }) {
  const { tx } = useTx();
  const { toggle } = useFeatures();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  // A switch the server refuses shakes, again on every refusal.
  const [shake, setShake] = useState(0);
  const shakeRef = useShake<HTMLDivElement>(shake);

  const { m, fold, settingUp, setSettingUp } = f;
  // Without the module in this build there is no switch, and its fields stand alone.
  if (!m || m.verdict !== 'shipped') return <>{children}</>;

  // Set up but not working, such as a reconnect method still missing its
  // router password: the server has nothing to switch until the card is done.
  const unfinished = m.switch === 'parked' && !m.enabled && !m.parked && !m.unset;
  const inert = busy || blocked || unfinished || m.switch === 'none';

  async function onSwitch(next: boolean) {
    // ToggleRow's `disabled` stops the pointer, not the keyboard.
    if (inert) return;
    // Nothing to bring back yet, so on opens the card to set it up.
    if (fold === 'unset') {
      setSettingUp(next);
      return;
    }
    setBusy(true);
    try {
      await toggle(id, next);
    } catch (e) {
      // The server refuses a switch it cannot honour and says why.
      toast(tx('settings.modules.switchFailed', { reason: switchRefusal(tx, e) }), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  // Without a switch, why not is the line under it.
  const detail = moduleDetail(tx, m);
  let state = m.switch === 'none' ? moduleReason(tx, m) : undefined;
  if (fold === 'unset') {
    state = settingUp ? tx('settings.modules.settingUp') : tx(`settings.modules.missing.${id}` as TranslationKey);
  } else if (fold === 'off' && m.switch === 'parked') {
    state = tx('settings.modules.parkedBelow');
  } else if (fold === 'off') {
    state = detail ?? (folds ? tx('settings.modules.offKept') : undefined);
  }

  return (
    <div className="flex flex-col gap-3">
      <div ref={shakeRef} data-new="module-switches" className="flex flex-col gap-1">
        <ToggleRow
          hue={hue}
          label={label(tx, 'settings.module.', id)}
          hint={[
            hint ?? '',
            unfinished ? tx('settings.modules.setUpBelow') : '',
            state === detail ? '' : (detail ?? ''),
          ]}
          checked={m.enabled || settingUp}
          disabled={inert}
          onChange={(next) => void onSwitch(next)}
        />
        {state && (
          <p dir="auto" className="text-meta text-carbon-textMuted">
            {state}
          </p>
        )}
      </div>
      {children}
    </div>
  );
}

/**
 * ModuleCard is a card that belongs to one module. Open, it is the card as the
 * page draws it with the switch under its title; off, it folds to the title,
 * the switch and a line on its state, and a heading action such as Add goes
 * with the rest.
 */
export function ModuleCard({
  id,
  hue,
  title,
  titleHint,
  titleRight,
  className = 'flex flex-col gap-5',
  hint,
  switchHue,
  blocked,
  'data-new': mark,
  children,
}: Omit<ToggleProps, 'hue'> & {
  hue: number;
  /** The card's title, the module's own name where it is left out. */
  title?: string;
  titleHint?: string;
  titleRight?: ReactNode;
  className?: string;
  /** The switch's own palette position, as ModuleToggle's `hue`. */
  switchHue?: number;
  'data-new'?: string;
  children?: ReactNode;
}) {
  const { tx } = useTx();
  const f = useModuleFold(id);
  return (
    <Card hue={hue} data-new={mark} className={className}>
      <SectionTitle hint={titleHint} right={f.open ? titleRight : undefined}>
        {title ?? label(tx, 'settings.module.', id)}
      </SectionTitle>
      <SwitchRow id={id} hue={switchHue} hint={hint} blocked={blocked} f={f} folds={children !== undefined} />
      {f.open && children}
    </Card>
  );
}

/**
 * jumpToSwitch asks for a module's switch on its page, as a row or as the title
 * of a card named after the module, and for the page alone where the settings
 * index knows neither.
 */
function jumpToSwitch(m: Feature) {
  const key = `settings.module.${m.id}` as TranslationKey;
  const card = SETTINGS_INDEX[m.page]?.find((c) => c.title === key || c.rows.some((r) => r.key === key));
  // A link to the page alone still retires a jump that is looking, or it
  // would mark something on the new page.
  if (card) requestJump({ page: m.page, title: card.title, label: key });
  else clearJump();
}

/**
 * ModuleSwitchBadge leads to a module's switch on its page, from somewhere the
 * module is off. `onFollow` runs once the switch is asked for, such as to close
 * the panel the badge is in.
 */
export function ModuleSwitchBadge({ m, title, onFollow }: { m: Feature; title: string; onFollow?: () => void }) {
  if (!m.page) return null;
  return (
    <PageBadge
      page={m.page}
      title={title}
      onFollow={() => {
        jumpToSwitch(m);
        onFollow?.();
      }}
    />
  );
}

/**
 * PageBadge leads to another settings page, a badge because everything
 * clickable is one (GlimStone rule 13). It wears that page's rail glyph and
 * follows the label engine the way LinkBadge does; where the words are hidden,
 * they are its bubble. `onFollow` runs before the page changes, to ask for the
 * row to land on.
 */
export function PageBadge({
  page,
  title,
  onFollow,
  'data-new': mark,
}: {
  page: string;
  title: string;
  onFollow?: () => void;
  /** The change this badge is marked as after an update (lib/whatsNew.ts). */
  'data-new'?: string;
}) {
  const labelMode = useLabelMode('buttons');
  const showText = labelMode === 'text' || labelMode === 'both';
  // The server may name a page this build has no glyph for.
  const Glyph = pageGlyph(page);
  const tip = useTooltip<HTMLAnchorElement>(showText ? null : title);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <Link
        to={`/settings/${page}`}
        onClick={onFollow}
        data-new={mark}
        aria-label={showText ? undefined : title}
        {...tipHoverProps}
        // Never wider than its row: on a phone a long page name is cut short
        // instead of running out of the card.
        className={`${linkBadgeClass(showText)} max-w-full`}
      >
        {labelMode !== 'text' && (
          <span className="glim-btn-glyph">
            {Glyph ? <Glyph aria-hidden /> : <IconForward aria-hidden className="rtl:-scale-x-100" />}
          </span>
        )}
        {showText && <span className="min-w-0 truncate">{title}</span>}
      </Link>
      {tip.node}
    </>
  );
}
