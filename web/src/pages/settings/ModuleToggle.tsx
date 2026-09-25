import { useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { linkBadgeClass, ToggleRow, useTooltip } from '../../components/ui';
import type { TranslationKey } from '../../lib/i18n';
import { IconChevronEnd } from '../../lib/icons';
import { useNavLabels } from '../../lib/navLabels';
import { useToast } from '../../lib/toast';
import { useFeatures } from './context';
import type { Feature, FeatureVerdict } from './features';
import { requestJump } from './jump';
import { pageGlyph } from './pageIcons';
import { label, moduleDetail, moduleReason, switchRefusal, useTx } from './tx';

/**
 * ModuleToggle is a module's switch on the page it is configured on. It goes
 * through the module registry rather than the draft, so it and the module's row
 * on the Modules page are one switch, and it leads to that row. It is left out
 * when the server has no such module.
 *
 * A module switched by parking its value, such as the watch folder, has nothing
 * to switch on until it is set up, and the switch is disabled with a word on
 * how to set it up. `blocked` is for a state only the page knows, such as an
 * edit that is not saved yet.
 */
export function ModuleToggle({
  id,
  hue = 0,
  hint,
  setUpHint,
  parkedHint,
  blocked = false,
  children,
}: {
  id: string;
  hue?: number;
  /** What the module does, the first paragraph of the switch's (i). */
  hint?: string;
  /** How to give a parked module its first value, where the generic sentence would be vague. */
  setUpHint?: string;
  /** What switching a parked module back on brings back, likewise. */
  parkedHint?: string;
  blocked?: boolean;
  /** The module's own field, under the switch. */
  children?: ReactNode;
}) {
  const { tx } = useTx();
  const { features, toggle } = useFeatures();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  // Keyed onto the row, so a switch the server refuses shakes again on every
  // refusal.
  const [shake, setShake] = useState(0);

  const m = features.modules.find((f) => f.id === id);
  if (!m || m.verdict !== 'shipped') return null;

  // The server would refuse to switch these on, since nothing is parked.
  const unset = m.switch === 'parked' && !m.enabled && !m.parked;
  const parked = m.switch === 'parked' && !m.enabled && m.parked;
  const inert = busy || blocked || unset || m.switch === 'none';

  async function onSwitch(next: boolean) {
    // ToggleRow's `disabled` stops the pointer, not the keyboard.
    if (inert) return;
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

  const row = (
    <div key={shake} className={shake > 0 ? 'glim-shake' : undefined}>
      <ToggleRow
        hue={hue}
        label={label(tx, 'settings.module.', id)}
        hint={[
          hint ?? '',
          m.switch === 'none' ? (moduleReason(tx, m) ?? '') : '',
          unset ? (setUpHint ?? tx('settings.modules.setUpBelow')) : '',
          parked ? (parkedHint ?? tx('settings.modules.parkedBelow')) : '',
          moduleDetail(tx, m) ?? '',
        ]}
        checked={m.enabled}
        disabled={inert}
        onChange={(next) => void onSwitch(next)}
        aside={<ModulesPageBadge m={m} />}
      />
    </div>
  );
  if (!children) return row;
  return (
    <div className="flex flex-col gap-3">
      {row}
      {children}
    </div>
  );
}

/**
 * moduleSection is the card of the Modules page a module's row is on, by the
 * title the settings search finds that card by.
 */
export function moduleSection(verdict: FeatureVerdict): TranslationKey {
  if (verdict === 'shipped') return 'settings.modules.sectionShipped';
  if (verdict === 'desktop') return 'settings.modules.sectionDesktop';
  return 'settings.modules.sectionNotBuilt';
}

/**
 * ModulesPageBadge leads to a module's row on the Modules page. Its words say
 * the switch is there as well, unless `title` says something else.
 */
export function ModulesPageBadge({ m, title }: { m: Feature; title?: string }) {
  const { tx } = useTx();
  return (
    <PageBadge
      page="modules"
      title={title ?? tx('settings.modules.alsoOn', { page: tx('settings.nav.modules') })}
      onFollow={() =>
        requestJump({
          page: 'modules',
          title: moduleSection(m.verdict),
          label: `settings.module.${m.id}` as TranslationKey,
        })
      }
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
export function PageBadge({ page, title, onFollow }: { page: string; title: string; onFollow?: () => void }) {
  const labelMode = useNavLabels();
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
        aria-label={showText ? undefined : title}
        {...tipHoverProps}
        className={linkBadgeClass(showText)}
      >
        {labelMode !== 'text' && (
          <span className="glim-btn-glyph">
            {Glyph ? <Glyph aria-hidden /> : <IconChevronEnd aria-hidden className="rtl:-scale-x-100" />}
          </span>
        )}
        {showText && <span className="whitespace-nowrap">{title}</span>}
      </Link>
      {tip.node}
    </>
  );
}
