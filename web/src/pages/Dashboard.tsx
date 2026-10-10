import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, type Settings, fetchInstances, fetchSettings } from '../lib/api';
import { hueVars } from '../lib/appearance';
import { useTasks } from '../lib/useTasks';
import { useResource } from '../lib/useResource';
import { fmtBytes, fmtRate, pct } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { refreshNeeds, useNeeds } from '../lib/useNeeds';
import {
  DEFAULT_LAYOUT,
  OVERVIEW_CARDS,
  SPANS,
  moved,
  pack,
  readLayout,
  shownCards,
  spanOf,
  withHidden,
  withOrder,
  withWidth,
  type CardSpan,
  type OverviewCard,
  type OverviewLayout,
} from '../lib/overviewLayout';
import { usePhoneLayout } from '../lib/phoneLayout';
import { useUIState } from '../lib/uistate';
import { Button, Card, PageHeader, SectionTitle, EmptyState } from '../components/ui';
import { SpeedGraph } from '../components/SpeedGraph';
import { VolumeCard } from '../components/VolumeCard';
import { Counters } from '../components/Counters';
import { DiskSpaceTile } from '../components/DiskSpaceTile';
import { ProgressBar } from '../components/ProgressBar';
import { StatusPill, rowState } from '../components/StatusPill';
import { InstanceRow } from '../components/InstanceCard';
import { TorrentCard } from '../components/TorrentCard';
import { NeedsCard } from '../components/NeedsCard';
import { OverviewHero } from '../components/OverviewHero';
import { PageAction, PageActions } from '../components/PageActions';
import { useAddLinks } from '../components/AddLinksAction';
import { Tabs } from '../components/Tabs';
import { useReorder } from '../components/dragLift';
import { IconArrowDown, IconArrowUp, IconCheck, IconDownloads, IconEdit, IconEye, IconEyeOff, IconGrip } from '../lib/icons';

const CARD_NAME: Record<OverviewCard, TranslationKey> = {
  needs: 'overview.needs.title',
  speed: 'overview.totalSpeed',
  recent: 'overview.recent',
  disk: 'disk.title',
  instances: 'overview.instances',
  torrents: 'overview.torrents.title',
  volume: 'volume.title',
};

// Figures rather than words, so the selector reads the same in every language.
const SPAN_NAME: Record<CardSpan, string> = { 2: '⅓', 3: '½', 4: '⅔', 6: '1' };

const sameSet = (a: ReadonlySet<string>, b: ReadonlySet<string>) => a.size === b.size && [...a].every((x) => b.has(x));

/**
 * useEmptyCards names the cards that draw nothing right now. Several cards
 * leave themselves out while they have nothing to show, and only the document
 * knows which, so the grid is watched rather than each card asked.
 */
function useEmptyCards(grid: RefObject<HTMLElement | null>): ReadonlySet<string> {
  const [empty, setEmpty] = useState<ReadonlySet<string>>(new Set());
  useLayoutEffect(() => {
    const el = grid.current;
    if (!el) return;
    const read = () => {
      const next = new Set<string>();
      for (const body of el.querySelectorAll<HTMLElement>('[data-ov-body]')) {
        if (body.childElementCount === 0) next.add(body.dataset.ovBody ?? '');
      }
      setEmpty((cur) => (sameSet(cur, next) ? cur : next));
    };
    read();
    const watch = new MutationObserver(read);
    watch.observe(el, { childList: true, subtree: true });
    return () => watch.disconnect();
  }, [grid]);
  return empty;
}

export function Dashboard() {
  const { t } = useT();
  const tasks = useTasks('');
  const { data: instances } = useResource<Instance[]>(fetchInstances);
  // Carries the configured instance name and the speed limit.
  const { data: settings } = useResource<Settings>(fetchSettings);
  const navigate = useNavigate();
  const phone = usePhoneLayout();
  const feed = useNeeds(tasks);
  useEffect(refreshNeeds, []);

  const [stored, setStored] = useUIState<OverviewLayout | null>('overview.cards', null);
  const layout = useMemo(() => readLayout(stored), [stored]);
  const [editing, setEditing] = useState(false);
  const addLinks = useAddLinks();
  const grid = useRef<HTMLDivElement>(null);
  const empty = useEmptyCards(grid);

  const list = useMemo(() => Object.values(tasks), [tasks]);
  const counts = useMemo(() => {
    let running = 0,
      queued = 0,
      done = 0,
      error = 0,
      collected = 0,
      speed = 0;
    for (const x of list) {
      if (x.status === 'running' || x.status === 'extracting') running++;
      else if (x.status === 'queued') queued++;
      else if (x.status === 'done') done++;
      else if (x.status === 'error') error++;
      // Links the filter holds back and rows a hoster preset set aside are in
      // no list and not counted.
      else if (x.status === 'collected' && !x.skipped && !x.variantOff) collected++;
      if (x.status === 'running') speed += x.speed;
    }
    return { running, queued, done, error, collected, speed };
  }, [list]);

  const recent = useMemo(
    () =>
      list
        .filter((x) => x.status !== 'collected')
        .sort((a, b) => (a.createdAt > b.createdAt ? -1 : 1))
        .slice(0, 6),
    [list],
  );

  // A phone keeps one order, with what needs the person first, and every card
  // takes the full width; the person's order and widths are for wider windows.
  const arranged = shownCards(layout);
  const drag = useReorder({
    ids: arranged,
    container: grid,
    attr: 'data-ov-card',
    axis: 'x',
    arm: 'move',
    enabled: editing && !phone,
    onReorder: (ids) => setStored(withOrder(layout, ids as OverviewCard[])),
  });
  const shown = phone ? OVERVIEW_CARDS.filter((id) => arranged.includes(id)) : (drag.order as OverviewCard[]);
  // A card with nothing to show keeps its place while the cards are arranged
  // and gives it up otherwise, so no row ends in a gap.
  const drawn = shown.filter((id) => editing || !empty.has(id));
  const spans = pack(drawn.map((id) => spanOf(layout, id)));

  function body(id: OverviewCard, hue: number): ReactNode {
    switch (id) {
      case 'needs':
        return <NeedsCard feed={feed} hue={hue} />;
      case 'speed':
        return (
          // A hand-rolled card, so the hue class and properties are set here
          // together, or `.glim-hue` resolves --accent to nothing.
          <div
            className="glim-card glim-hue grid grid-cols-1 items-center gap-4 overflow-hidden p-5 sm:grid-cols-[auto_minmax(0,1fr)] sm:gap-8"
            style={hueVars(hue) as CSSProperties}
          >
            <div>
              <div className="glim-eyebrow">{t('overview.totalSpeed')}</div>
              <div className="glim-num mt-1 text-[38px] font-semibold leading-none tracking-tight text-carbon-text">
                {fmtRate(counts.speed)}
              </div>
              <div className="mt-4">
                <Counters counts={counts} />
              </div>
            </div>
            {/* The limit feeds the 1337 easter egg (docs/easter-eggs.md). */}
            <SpeedGraph value={counts.speed} height={96} limit={settings?.speedLimit ?? 0} />
          </div>
        );
      case 'recent':
        return (
          // SectionTitle's badge is positioned against a `.glim-card`.
          <Card hue={hue} className="flex flex-col gap-3">
            <SectionTitle>{t('overview.recent')}</SectionTitle>
            {recent.length === 0 ? (
              <EmptyState nested icon={<IconDownloads width={26} height={26} />} title={t('overview.noDownloads')} />
            ) : (
              <div className="glim-well divide-y divide-carbon-border/60 p-0">
                {recent.map((x) => (
                  <div key={x.id} className="flex items-center gap-4 px-5 py-3">
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm text-carbon-text">{x.name || x.url}</div>
                      <div className="mt-1.5 max-w-xs">
                        <ProgressBar
                          percent={pct(x.loaded, x.size, x.status === 'done')}
                          active={x.status !== 'error'}
                          indeterminate={x.status === 'running' && x.size <= 0}
                          moving={x.status === 'running' || x.status === 'extracting'}
                          tone={x.status === 'done' ? 'ok' : 'accent'}
                        />
                      </div>
                    </div>
                    <span className="glim-num text-xs text-carbon-textSub">{fmtBytes(x.size)}</span>
                    <StatusPill status={rowState(x)} />
                  </div>
                ))}
              </div>
            )}
          </Card>
        );
      case 'instances':
        return (
          <Card hue={hue} className="flex flex-col gap-3">
            <SectionTitle>{t('overview.instances')}</SectionTitle>
            <div className="glim-well divide-y divide-carbon-border/60 p-0">
              <InstanceRow name={settings?.instanceName || t('instances.thisInstance')} base="/api" />
              {(instances ?? []).map((i) => (
                <InstanceRow
                  key={i.name}
                  name={i.displayName ?? i.name}
                  base={`/api/instances/${encodeURIComponent(i.name)}`}
                  onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(i.name)}`)}
                />
              ))}
            </div>
          </Card>
        );
      case 'torrents':
        return <TorrentCard settings={settings} hue={hue} />;
      case 'volume':
        return <VolumeCard hue={hue} />;
      case 'disk':
        return <DiskSpaceTile settings={settings} hue={hue} />;
    }
  }

  const step = (id: OverviewCard, by: -1 | 1) => setStored(moved(layout, id, by));
  const bar = (id: OverviewCard, at: number) => (
    <div className="mb-[18px] flex flex-wrap items-center gap-1.5">
      {!phone && (
        // The buttons beside it move the card for a keyboard.
        <span
          aria-hidden
          onPointerDown={(e) => drag.press(e, id)}
          className="flex h-[var(--btn-h)] w-6 shrink-0 cursor-grab touch-none items-center justify-center text-carbon-textMuted"
        >
          <IconGrip width={14} height={14} />
        </span>
      )}
      <span className="glim-eyebrow min-w-0 flex-1 truncate">{t(CARD_NAME[id])}</span>
      {!phone && (
        <>
          <Button
            kind="ghost"
            icon={<IconArrowUp />}
            title={t('task.moveUp')}
            disabled={at === 0}
            onClick={() => step(id, -1)}
          />
          <Button
            kind="ghost"
            icon={<IconArrowDown />}
            title={t('task.moveDown')}
            disabled={at === drawn.length - 1}
            onClick={() => step(id, 1)}
          />
          <Tabs
            variant="well"
            size="sm"
            inline
            label={t('overview.customize.width')}
            items={SPANS.map((s) => ({ id: String(s), label: SPAN_NAME[s] }))}
            active={String(spanOf(layout, id))}
            onSelect={(s) => setStored(withWidth(layout, id, Number(s) as CardSpan))}
          />
        </>
      )}
      <Button
        kind="ghost"
        labelled
        icon={<IconEyeOff />}
        title={t('common.hide')}
        onClick={() => setStored(withHidden(layout, id, true))}
      />
    </div>
  );

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('overview.title')} />

      <OverviewHero tasks={list} needs={feed.needs} done={feed.done} />

      {editing && (
        <div className="flex flex-wrap items-center justify-end gap-3">
          {!phone && (
            <p className="min-w-0 flex-1 basis-64 text-[13px] text-carbon-textMuted">{t('overview.customize.hint')}</p>
          )}
          <Button kind="secondary" onClick={() => setStored(DEFAULT_LAYOUT)}>
            {t('overview.customize.reset')}
          </Button>
        </div>
      )}

      <div
        ref={grid}
        className={`relative grid grid-cols-1 gap-10 md:grid-cols-6 ${drag.held !== null ? 'glim-drag-armed' : ''}`}
      >
        {shown.map((id, hue) => {
          // Every card stays mounted in its own cell, drawn or not, so one
          // that comes to have something to show is seen and takes its place.
          const at = drawn.indexOf(id);
          return (
            <div
              key={id}
              data-ov-card={id}
              className={`min-w-0 flex-col ${at < 0 ? 'hidden' : 'flex'} ${drag.look(id)} ${
                editing ? 'rounded-[var(--radius-card)] outline-2 outline-offset-[6px] outline-dashed outline-carbon-border' : ''
              }`}
              style={phone || at < 0 ? undefined : { gridColumn: `span ${spans[at]}` }}
            >
              {editing && bar(id, at)}
              {/* Cards in a row share its height. */}
              <div data-ov-body={id} className="flex grow flex-col empty:hidden [&>*]:grow">
                {body(id, hue)}
              </div>
              {editing && empty.has(id) && (
                <Card hue={hue} className="flex grow flex-col gap-3">
                  <SectionTitle>{t(CARD_NAME[id])}</SectionTitle>
                  <p className="text-sm text-carbon-textMuted">{t('overview.customize.empty')}</p>
                </Card>
              )}
            </div>
          );
        })}
      </div>

      {editing && layout.hidden.length > 0 && (
        <div className="relative rounded-[var(--radius-card)] border border-dashed border-carbon-border px-4 pb-4 pt-6">
          <SectionTitle>{t('overview.customize.hidden')}</SectionTitle>
          <div className="flex flex-wrap gap-2">
            {layout.hidden.map((id) => (
              <Button
                key={id}
                kind="secondary"
                icon={<IconEye />}
                title={t('common.show')}
                onClick={() => setStored(withHidden(layout, id, false))}
              >
                {t(CARD_NAME[id])}
              </Button>
            ))}
          </div>
        </div>
      )}

      <PageActions>
        <PageAction
          icon={editing ? <IconCheck /> : <IconEdit />}
          label={t(editing ? 'overview.customize.finish' : 'overview.customize')}
          onClick={() => setEditing((on) => !on)}
        />
        {addLinks.action}
      </PageActions>
      {addLinks.dialog}
    </div>
  );
}
