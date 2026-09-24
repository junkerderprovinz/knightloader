import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Card, InfoBubble, SectionTitle, useTooltip } from '../../components/ui';
import type { TranslationKey } from '../../lib/i18n';
import { IconChevronEnd } from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { NeutralSwitch } from './controls';
import { useFeatures } from './context';
import type { Feature } from './features';
import { clearJump, requestJump } from './jump';
import { SETTINGS_INDEX } from './searchIndex';
import { label, moduleDetail, useTx } from './tx';

/**
 * Modules lists what this build contains, with a switch on every module the
 * build has. The verdicts are separated by section rather than by hue, since
 * the accent means activity and nearly every row is simply enabled.
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
  const detailText = moduleDetail(tx, m);
  const detail = useTooltip<HTMLSpanElement>(detailText);

  const shipped = m.verdict === 'shipped';
  // A parked switch with nothing parked would answer 400, so it is disabled
  // with a reason that names the page where the value is set. A row without
  // a switch (no JD wired, no yt-dlp found) shows it disabled with the
  // server's reason.
  const nothingToRestore = m.switch === 'parked' && !m.enabled && !m.parked;
  const blocked = m.switch === 'none' || nothingToRestore;
  const reason = nothingToRestore
    ? tx('settings.modules.configureFirst', { page: label(tx, 'settings.nav.', m.page) })
    : m.reason;
  const dimmed = !m.enabled && shipped;
  const page = m.page !== 'modules' ? m.page : '';

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
      className={`flex items-center gap-3 rounded-[var(--radius-control)] px-3 py-2.5 transition-opacity hover:bg-carbon-hover ${
        dimmed ? 'opacity-55' : ''
      }`}
    >
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex items-center text-sm text-carbon-text">
          {label(tx, 'settings.module.', m.id)}
          {reason && <InfoBubble tip={reason} />}
        </span>
        {detailText && (
          // Truncated so a long path does not push the switch down a line; the
          // tooltip carries the whole of it.
          <span {...detail.triggerProps} className="truncate text-[11px] text-carbon-textMuted" dir="auto">
            {detailText}
          </span>
        )}
        {page && (
          <Link
            to={`/settings/${page}`}
            onClick={() => jumpToSwitch(m.id, page)}
            className="flex w-fit items-center gap-1 text-[11px] text-carbon-textSub underline-offset-2 hover:text-carbon-text hover:underline focus-visible:underline"
          >
            {tx('settings.modules.configuredOn', { page: label(tx, 'settings.nav.', page) })}
            <IconChevronEnd className="h-3 w-3 rtl:-scale-x-100" aria-hidden />
          </Link>
        )}
      </div>

      {shipped ? (
        <NeutralSwitch
          on={m.enabled}
          disabled={busy || blocked}
          name={label(tx, 'settings.module.', m.id)}
          onChange={onToggle}
          hue={hue}
        />
      ) : (
        <StateChip m={m} />
      )}
      {detail.node}
    </div>
  );
}

/**
 * jumpToSwitch makes a row's page link land on the module's own switch where
 * the settings index knows its row, and on the page alone otherwise.
 */
function jumpToSwitch(id: string, page: string) {
  const key = `settings.module.${id}` as TranslationKey;
  const card = SETTINGS_INDEX[page]?.find((c) => c.rows.some((r) => r.key === key));
  // A link to the page alone still retires a jump that is looking, or it
  // would mark something on the new page.
  if (card) requestJump({ page, title: card.title, label: key });
  else clearJump();
}

/**
 * StateChip names why a module is not in this build in one neutral word. A
 * missing subsystem is a scope decision, not a fault, so it gets no status hue.
 */
function StateChip({ m }: { m: Feature }) {
  const { tx } = useTx();
  return (
    <span className="shrink-0 rounded-[var(--radius-pill)] bg-carbon-surface2 px-2 py-1 text-[11px] font-medium text-carbon-textSub">
      {tx(m.verdict === 'desktop' ? 'settings.modules.desktopOnly' : 'settings.modules.notBuilt')}
    </span>
  );
}
