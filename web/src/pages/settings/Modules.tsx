import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Card, InfoBubble, SectionTitle } from '../../components/ui';
import { useToast } from '../../lib/toast';
import { NeutralSwitch } from './controls';
import { useFeatures } from './context';
import type { Feature } from './features';
import { label, useTx } from './tx';

/**
 * The modules page: an inventory of what this build contains, with a switch
 * only where switching it does something.
 *
 * Deliberately calm. Rule 3 of the design language reserves the accent for
 * activity, and "enabled" is the resting state of nearly every row here — a
 * column of gold switches would say six things are happening when the answer is
 * that nothing in particular is. Wave 1 had to un-gold exactly such a column on
 * the task list. So the switches are neutral, the three verdicts are separated
 * by section rather than by hue, and no row on this page is ever the accent.
 */
export function Modules() {
  const { tx } = useTx();
  const { features } = useFeatures();

  const shipped = features.modules.filter((m) => m.verdict === 'shipped');
  const desktop = features.modules.filter((m) => m.verdict === 'desktop');
  // Anything the server invents later lands here rather than disappearing: an
  // unrecognised verdict is still a subsystem the user should be told about.
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
    <Card className="flex flex-col gap-1 p-2">
      <SectionTitle hue={hue} hint={hint}>
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
  // A parked switch with nothing parked has nothing to restore, so it is offered
  // disabled with the reason rather than as a control that answers 400. The
  // reason names the page the value is set on, which is the only thing that gets
  // anybody out of this state.
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
      // The server refuses a switch it cannot honour and says why. Showing that
      // sentence is the point: a switch that silently snaps back is the failure
      // mode this whole registry exists to avoid.
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
          {/* The reason lives behind the (i), not under the row. Eighteen rows
              each carrying two lines of grey prose is the wall rule 8 forbids,
              and the state word beside it already carries the verdict. */}
          {(blockedReason ?? m.reason) && <InfoBubble tip={blockedReason ?? m.reason ?? ''} />}
        </span>
        {m.detail && (
          // Truncated, not wrapped: a watch folder is a long absolute path, and
          // letting it wrap pushes the switch down a line and makes eighteen rows
          // of different heights out of a list meant to be scanned.
          <span className="truncate text-[11px] text-carbon-textMuted" dir="ltr" title={m.detail}>
            {m.detail}
          </span>
        )}
      </div>

      {/* Where the module is actually configured. On hover, per the design
          language's rule about long lists: eighteen rows each carrying a
          permanent grey link is a column of furniture, and the switch beside it
          is what the row is for. Focus reveals it too, so it stays reachable by
          keyboard. */}
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
 * The state of a row that has no switch, as one neutral word.
 *
 * Neutral for all of them on purpose. The four state hues mean running,
 * settled, fault and waiting; "this build does not contain a captcha solver" is
 * none of those, and painting it red would make a deliberate scope decision look
 * like something is broken.
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
    <span
      className="shrink-0 rounded-[var(--radius-pill)] bg-carbon-surface2 px-2 py-1 text-[11px] font-medium text-carbon-textSub"
      title={m.verdict === 'shipped' ? tx('settings.modules.noSwitch') : undefined}
    >
      {text}
    </span>
  );
}
