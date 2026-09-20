import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Card, InfoBubble, SectionTitle } from '../../components/ui';
import { useToast } from '../../lib/toast';
import { NeutralSwitch } from './controls';
import { useFeatures } from './context';
import type { Feature } from './features';
import { label, useTx } from './tx';

/**
 * Modules lists what this build contains, with a switch only where switching
 * does something. The verdicts are separated by section rather than by hue,
 * since the accent means activity and nearly every row is simply enabled.
 */
export function Modules() {
  const { tx } = useTx();
  const { features } = useFeatures();

  const shipped = features.modules.filter((m) => m.verdict === 'shipped');
  const desktop = features.modules.filter((m) => m.verdict === 'desktop');
  // An unknown verdict from a newer server lands here instead of vanishing.
  const absent = features.modules.filter((m) => m.verdict !== 'shipped' && m.verdict !== 'desktop');

  return (
    <div className="flex flex-col gap-10">
      <Group hue={0} title={tx('settings.modules.sectionShipped')} hint={tx('settings.modules.fixedAtBuild')} rows={shipped} />
      <Group hue={1} title={tx('settings.modules.sectionDesktop')} rows={desktop} />
      <Group hue={2} title={tx('settings.modules.sectionNotBuilt')} rows={absent} />
    </div>
  );
}

function Group({ hue, title, hint, rows }: { hue: number; title: string; hint?: string; rows: Feature[] }) {
  if (rows.length === 0) return null;
  return (
    <Card hue={hue} className="flex flex-col gap-1 p-2">
      <SectionTitle hint={hint}>
        {title}
      </SectionTitle>
      {rows.map((m, i) => (
        <Row key={m.id} m={m} hue={i} />
      ))}
    </Card>
  );
}

function Row({ m, hue }: { m: Feature; hue: number }) {
  const { tx } = useTx();
  const { toggle } = useFeatures();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);

  const switchable = m.verdict === 'shipped' && m.switch !== 'none';
  // A parked switch with nothing parked would answer 400, so it is disabled
  // with a reason that names the page where the value is set.
  const nothingToRestore = m.switch === 'parked' && !m.enabled && !m.parked;
  const blockedReason = nothingToRestore
    ? tx('settings.modules.configureFirst', { page: label(tx, 'settings.nav.', m.page) })
    : undefined;
  const dimmed = !m.enabled && m.verdict === 'shipped';

  async function onToggle(next: boolean) {
    setBusy(true);
    try {
      await toggle(m.id, next);
    } catch (e) {
      // The server refuses a switch it cannot honour and says why.
      toast(tx('settings.modules.switchFailed', { reason: String(e).replace(/^Error:\s*/, '') }), 'fail');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className={`group flex items-center gap-3 rounded-[var(--radius-control)] px-3 py-2.5 transition-opacity hover:bg-carbon-hover ${
        dimmed ? 'opacity-55' : ''
      }`}
    >
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex items-center text-sm text-carbon-text">
          {label(tx, 'settings.module.', m.id)}
          {(blockedReason ?? m.reason) && <InfoBubble tip={blockedReason ?? m.reason ?? ''} />}
        </span>
        {m.detail && (
          // Truncated so a long path does not push the switch down a line.
          <span className="truncate text-[11px] text-carbon-textMuted" dir="ltr" title={m.detail}>
            {m.detail}
          </span>
        )}
      </div>

      {/* The link to the module's page shows on hover and on focus. */}
      {m.page && m.page !== 'modules' && (
        <Link
          to={`/settings/${m.page}`}
          className="hidden shrink-0 text-[11px] text-carbon-textMuted underline-offset-2 opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100 hover:text-carbon-text hover:underline sm:inline"
        >
          {tx('settings.modules.configuredOn', { page: label(tx, 'settings.nav.', m.page) })}
        </Link>
      )}

      {switchable ? (
        <NeutralSwitch
          on={m.enabled}
          disabled={busy || nothingToRestore}
          name={label(tx, 'settings.module.', m.id)}
          onChange={onToggle}
          hue={hue}
        />
      ) : (
        <StateChip m={m} />
      )}
    </div>
  );
}

/**
 * StateChip names the state of a row without a switch in one neutral word. A
 * missing subsystem is a scope decision, not a fault, so it gets no status hue.
 */
function StateChip({ m }: { m: Feature }) {
  const { tx } = useTx();
  const text =
    m.verdict === 'desktop'
      ? tx('settings.modules.desktopOnly')
      : m.verdict !== 'shipped'
        ? tx('settings.modules.notBuilt')
        : tx(m.enabled ? 'settings.modules.on' : 'settings.modules.off');
  return (
    <span className="flex shrink-0 items-center">
      <span className="rounded-[var(--radius-pill)] bg-carbon-surface2 px-2 py-1 text-[11px] font-medium text-carbon-textSub">
        {text}
      </span>
      {m.verdict === 'shipped' && <InfoBubble tip={tx('settings.modules.noSwitch')} />}
    </span>
  );
}
