import { useEffect, useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  IconBadge,
  LabelBadge,
  NumberInput,
  SectionTitle,
  TextInput,
  useTooltip,
} from '../../../components/ui';
import { PathInput } from '../../../components/FolderPicker';
import { Tabs } from '../../../components/Tabs';
import { IconFilter, IconFolder, IconPlus, IconPriority, IconTrash } from '../../../lib/icons';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  fetchFeeds,
  priorityChoices,
  testFeed,
  type FeedStatus,
  type FeedSubscription,
  type FeedTest,
  type PriorityChoice,
} from '../../../lib/api';
import { happened } from '../../../lib/countdown';
import { useDraft, useFeatures } from '../context';

// Feeds lists the RSS and Atom subscriptions this instance follows, each
// fetched on its own timer, with new entries handed to the collector like a
// pasted link.
//
// The address is the identity: the poller and the record of added entries are
// keyed on it, so a stored row's address is read only and changing it means
// removing the row and adding another. The health comes from GET /api/feeds,
// kept in memory, so after a restart a feed reads as not checked yet. The test
// only reads. A new row stays out of the draft until its address is valid, and
// the title filter is committed only once it compiles, because the server
// refuses the whole settings document over one bad row. An interval of 0 means
// the server's quarter of an hour, and an absent priority differs from 0.

/** feed.DefaultIntervalMinutes, what a stored 0 resolves to. */
const DEFAULT_INTERVAL_MINUTES = 15;
/** The range feed.Sanitize pulls a non-zero interval into. */
const MIN_INTERVAL_MINUTES = 1;
const MAX_INTERVAL_MINUTES = 10080;

/** The priority tab that removes the key rather than setting a value. */
const NO_PRIORITY = 'unset';

/**
 * usableAddress asks feed.Validate's three questions: does it parse, is the
 * scheme http or https, does it name a host. It is the loose half of the check,
 * kept here because a refusal would reject the whole settings document.
 */
function usableAddress(raw: string): boolean {
  try {
    const u = new URL(raw);
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== '';
  } catch {
    return false;
  }
}

/**
 * compiles catches a title filter JavaScript cannot parse. RE2 refuses more
 * than RegExp, so a pattern that passes may still be refused at save, but
 * nothing the server accepts is blocked here.
 */
function compiles(pattern: string): boolean {
  try {
    new RegExp(pattern);
    return true;
  } catch {
    return false;
  }
}

/** clampInterval keeps 0 and cuts anything else into the range feed.Sanitize uses. */
function clampInterval(v: number): number {
  if (!Number.isFinite(v)) return 0;
  const whole = Math.round(v);
  if (whole === 0) return 0;
  return Math.min(Math.max(whole, MIN_INTERVAL_MINUTES), MAX_INTERVAL_MINUTES);
}

/**
 * without removes one optional key from a row. Absent and empty mean the same
 * for the folder and the filter, but not for the priority, where 0 is a value.
 */
function without(row: FeedSubscription, key: 'titleFilter' | 'dir' | 'priority'): FeedSubscription {
  const next = { ...row };
  delete next[key];
  return next;
}

/** What committing a typed address did, so the row can say why nothing happened. */
type Verdict = 'ok' | 'blank' | 'invalid' | 'duplicate';

/**
 * PendingRow is a new row without a usable address. It stays out of the draft,
 * where feed.Sanitize would delete it on the next autosave.
 */
interface PendingRow {
  id: string;
  row: FeedSubscription;
}

let pendingCounter = 0;
const freshId = () => `p${(pendingCounter++).toString(36)}`;

// Row ids: a stored row by its address, a new one by a client id, kept apart
// by prefix since an address can be anything somebody types.
const storedId = (url: string) => `u:${url}`;

/**
 * usePriorityTabs builds the priority strip from /api/queue/priorities behind a
 * "not set" item, and stays empty until the ladder arrives.
 */
function usePriorityTabs(): { id: string; label: string }[] {
  const { t } = useT();
  const [choices, setChoices] = useState<PriorityChoice[]>([]);
  useEffect(() => {
    let live = true;
    void priorityChoices().then(
      (p) => {
        if (live) setChoices(p);
      },
      () => {
        /* the strip stays out rather than offering a guess at the ladder */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  if (choices.length === 0) return [];
  return [
    // Its own word, which the field's hint refers to.
    { id: NO_PRIORITY, label: t('settings.feeds.priorityNone') },
    ...choices
      .slice()
      .reverse()
      .map((p) => ({ id: String(p.value), label: t(`priority.${p.id}` as TranslationKey) })),
  ];
}

export function FeedsCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const { features } = useFeatures();

  // feed.Sanitize answers nil for an empty list.
  const rows = cfg.feeds ?? [];
  const priorities = usePriorityTabs();

  // The Modules page parks the rows and clears the list. Checked with `parked`,
  // because an empty list on a fresh install also reads as off.
  const feedsModule = features.modules.find((m) => m.id === 'feeds');
  const parked = feedsModule !== undefined && !feedsModule.enabled && feedsModule.parked;

  const [openRow, setOpenRow] = useState('');
  const [pending, setPending] = useState<PendingRow[]>([]);

  // Fetched once, keyed by address: it changes on each feed's own timer. A
  // failure leaves it empty, which reads as "nothing to report yet".
  const [health, setHealth] = useState<Record<string, FeedStatus>>({});
  useEffect(() => {
    let alive = true;
    void fetchFeeds().then(
      (list) => {
        if (!alive) return;
        const byURL: Record<string, FeedStatus> = {};
        for (const s of list) byURL[s.url] = s;
        setHealth(byURL);
      },
      () => {
        /* No status is drawn when nothing answered. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  // An emptied list goes out as [], never undefined, which the shell's diff
  // would send as a key without a value.
  const write = (next: FeedSubscription[]) => patch({ feeds: next });

  /**
   * Moves a pending row into the draft under the typed address, refusing
   * anything the server would reject or merge away.
   */
  const commit = (typed: string, row: FeedSubscription): Verdict => {
    const url = typed.trim();
    if (url === '') return 'blank';
    if (!usableAddress(url)) return 'invalid';
    // A second row for one address would be merged away on save.
    if (rows.some((r) => r.url.trim() === url)) return 'duplicate';
    write([...rows, { ...row, url }]);
    return 'ok';
  };

  const add = () => {
    const row: PendingRow = { id: freshId(), row: { url: '', intervalMinutes: 0 } };
    setPending((p) => [...p, row]);
    setOpenRow(row.id);
  };

  const rowCount = rows.length + pending.length;

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.feeds.titleHint')}
        right={
          <div className="flex items-center gap-2">
            {/* While the module is off the badge replaces the Add button. */}
            {parked && <LabelBadge label={t('settings.modules.off')} />}
            {!parked && (
              <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
                {t('settings.feeds.add')}
              </Button>
            )}
          </div>
        }
      >
        {t('settings.feeds.title')}
      </SectionTitle>

      {/* Parking clears the list on the server, so while the module is off
          only the empty-state sentence remains. */}
      {rowCount === 0 ? (
        // Inside the card rather than an EmptyState, which would hide Add.
        <p className="py-6 text-center text-sm text-carbon-textSub">
          {t('settings.feeds.empty')}
          <span className="mt-1 block text-[11px] text-carbon-textMuted">{t('settings.feeds.emptyHint')}</span>
        </p>
      ) : (
        <ul className="flex flex-col">
          {rows.map((row, i) => (
            <FeedRow
              key={storedId(row.url)}
              row={row}
              index={i}
              last={i === rowCount - 1}
              stored
              priorities={priorities}
              status={health[row.url]}
              open={openRow === storedId(row.url)}
              onToggle={() => setOpenRow(openRow === storedId(row.url) ? '' : storedId(row.url))}
              onCommitUrl={() => 'ok'}
              onChange={(next) => write(rows.map((r) => (r.url === row.url ? next : r)))}
              onRemove={() => write(rows.filter((r) => r.url !== row.url))}
            />
          ))}
          {pending.map((p, i) => (
            <FeedRow
              key={p.id}
              row={p.row}
              index={rows.length + i}
              last={rows.length + i === rowCount - 1}
              stored={false}
              priorities={priorities}
              open={openRow === p.id}
              onToggle={() => setOpenRow(openRow === p.id ? '' : p.id)}
              onCommitUrl={(typed) => {
                // Kept so collapsing a half-typed row keeps the text.
                setPending((list) =>
                  list.map((r) => (r.id === p.id ? { ...r, row: { ...r.row, url: typed } } : r)),
                );
                const verdict = commit(typed, p.row);
                if (verdict === 'ok') {
                  setPending((list) => list.filter((r) => r.id !== p.id));
                  // The stored row is keyed by its address, so it remounts; keep it open.
                  setOpenRow(storedId(typed.trim()));
                }
                return verdict;
              }}
              onChange={(next) => setPending((list) => list.map((r) => (r.id === p.id ? { ...r, row: next } : r)))}
              onRemove={() => setPending((list) => list.filter((r) => r.id !== p.id))}
            />
          ))}
        </ul>
      )}
    </Card>
  );
}

/**
 * FeedRow shows one subscription, collapsed to its address and overrides,
 * expanded to the five fields. A new row's address reaches the draft only once
 * it is valid.
 */
function FeedRow({
  row,
  index,
  last,
  stored,
  open,
  priorities,
  status,
  onToggle,
  onCommitUrl,
  onChange,
  onRemove,
}: {
  row: FeedSubscription;
  index: number;
  last: boolean;
  /** Already in the draft, so the subscription has a memory. */
  stored: boolean;
  open: boolean;
  priorities: { id: string; label: string }[];
  onToggle: () => void;
  status?: FeedStatus;
  onCommitUrl: (typed: string) => Verdict;
  onChange: (next: FeedSubscription) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  const [text, setText] = useState(row.url);
  const [verdict, setVerdict] = useState<Verdict>('blank');

  // Committed on blur, like the address, since a pattern that does not compile
  // makes the server refuse the whole document.
  const [filter, setFilter] = useState(row.titleFilter ?? '');
  const [filterBad, setFilterBad] = useState(false);

  // The save answer replaces the draft and may trim the filter, so follow it.
  useEffect(() => {
    setFilter(row.titleFilter ?? '');
    setFilterBad(false);
  }, [row.titleFilter]);

  const commitUrl = () => setVerdict(onCommitUrl(text));

  const commitFilter = () => {
    const next = filter.trim();
    // An identical patch would still mark the draft dirty.
    if (next === (row.titleFilter ?? '')) {
      setFilterBad(false);
      return;
    }
    if (next === '') {
      setFilterBad(false);
      onChange(without(row, 'titleFilter'));
      return;
    }
    if (!compiles(next)) {
      setFilterBad(true);
      return;
    }
    setFilterBad(false);
    onChange({ ...row, titleFilter: next });
  };

  // A stored 0 runs at the server's quarter of an hour, shown quieter than a
  // typed number.
  const derived = row.intervalMinutes === 0;
  const effective = derived ? DEFAULT_INTERVAL_MINUTES : row.intervalMinutes;

  // The house tooltip rather than a native title. Both spans sit inside the
  // row's expand button, so they take no role and no tab stop of their own.
  const urlTip = useTooltip<HTMLSpanElement>(row.url);
  const { role: _urlRole, tabIndex: _urlTabIndex, ...urlTipProps } = urlTip.triggerProps;
  const intervalTip = useTooltip<HTMLSpanElement>(t('settings.feeds.interval'));
  const { role: _ivRole, tabIndex: _ivTabIndex, ...intervalTipProps } = intervalTip.triggerProps;

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[1fr_auto] items-center gap-3 py-2.5">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          className="flex min-w-0 items-center gap-3 text-left"
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          {/* Truncated, with the whole address in the tooltip. */}
          <span dir="ltr" {...urlTipProps} className="min-w-0 flex-1 truncate text-sm text-carbon-text">
            {row.url || <span className="text-carbon-textMuted">{t('settings.feeds.url')}</span>}
          </span>
          {/* What this row overrides, by field. */}
          {row.titleFilter && <Marker icon={<IconFilter width={14} height={14} />} title={t('settings.feeds.filter')} />}
          {row.dir && <Marker icon={<IconFolder width={14} height={14} />} title={t('settings.feeds.dir')} />}
          {row.priority !== undefined && (
            <Marker icon={<IconPriority width={14} height={14} />} title={t('settings.feeds.priority')} />
          )}
          <span
            {...intervalTipProps}
            className={`glim-num hidden w-12 shrink-0 text-end text-xs sm:block ${
              derived ? 'text-carbon-textMuted' : 'text-carbon-textSub'
            }`}
          >
            {effective}
          </span>
        </button>
        {urlTip.node}
        {intervalTip.node}
        <div className="flex items-center gap-1.5">
          <IconBadge
            // A lone glyph takes half its 32px badge.
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={t('settings.feeds.remove')}
            aria-label={t('settings.feeds.remove')}
            // No blur, so no commit happens in front of the removal.
            onMouseDown={(e) => e.preventDefault()}
            onClick={onRemove}
          />
        </div>
      </div>

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          <Field label={t('settings.feeds.url')} hint={t('settings.feeds.urlHint')}>
            {stored ? (
              // Read only rather than absent, so the address can still be copied.
              <TextInput
                dir="ltr"
                readOnly
                spellCheck={false}
                value={row.url}
                // Opacity rather than a quieter ink, since two text colours on
                // one element resolve by stylesheet order.
                className="cursor-default opacity-70"
              />
            ) : (
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={text}
                placeholder="https://example.org/feed.xml"
                // Marked, not corrected; the focus ring covers the halo while typing.
                aria-invalid={verdict === 'invalid' || verdict === 'duplicate'}
                className={
                  verdict === 'invalid' || verdict === 'duplicate'
                    ? 'shadow-[0_0_0_2px_var(--status-warn-text)]'
                    : ''
                }
                onChange={(e) => {
                  setText(e.target.value);
                  setVerdict('blank');
                }}
                onBlur={commitUrl}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    e.currentTarget.blur();
                  }
                }}
              />
            )}
          </Field>
          {verdict === 'duplicate' && <p className="text-xs text-statusWarn">{t('settings.feeds.duplicate')}</p>}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {/* 0 takes the server's quarter of an hour; other values are cut
                into 1..10080 as feed.Sanitize does. */}
            <Field label={t('settings.feeds.interval')} hint={t('settings.feeds.intervalHint')}>
              <NumberInput
                value={row.intervalMinutes}
                min={0}
                max={MAX_INTERVAL_MINUTES}
                step={1}
                onValue={(v) => onChange({ ...row, intervalMinutes: clampInterval(v) })}
              />
            </Field>
            <Field label={t('settings.feeds.filter')} hint={t('settings.feeds.filterHint')}>
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={filter}
                // Marked and not committed, so a broken pattern never reaches
                // the draft.
                aria-invalid={filterBad}
                className={filterBad ? 'shadow-[0_0_0_2px_var(--status-warn-text)]' : ''}
                onChange={(e) => {
                  setFilter(e.target.value);
                  setFilterBad(false);
                }}
                onBlur={commitFilter}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    e.currentTarget.blur();
                  }
                }}
              />
            </Field>
          </div>

          {/* PathInput, since the folder may be a template. It is checked only
              when an entry arrives, and a failure there drops the priority too,
              since both are written in one call. */}
          <Field label={t('settings.feeds.dir')} hint={t('settings.feeds.dirHint')}>
            <PathInput
              value={row.dir ?? ''}
              title={t('settings.feeds.dir')}
              onValue={(dir) => onChange(dir.trim() === '' ? without(row, 'dir') : { ...row, dir })}
            />
          </Field>

          {/* FieldGroup, because a Field's label would pass a click on the
              caption to the first tab. */}
          {priorities.length > 0 && (
            <FieldGroup label={t('settings.feeds.priority')} hint={t('settings.feeds.priorityHint')}>
              <Tabs
                size="sm"
                label={t('settings.feeds.priority')}
                  // Matched on undefined, since 0 is a real priority.
                active={row.priority === undefined ? NO_PRIORITY : String(row.priority)}
                onSelect={(id) =>
                  onChange(id === NO_PRIORITY ? without(row, 'priority') : { ...row, priority: Number(id) })
                }
                items={priorities}
              />
            </FieldGroup>
          )}

          {stored && <FeedHealth status={status} />}
          <FeedProbe url={stored ? row.url : text} filter={filter} />
        </div>
      )}
    </li>
  );
}

/**
 * FeedHealth shows what the server knows about a stored subscription. The
 * table lives in memory, so no lastPolledAt means not looked at since the
 * server started, and the counters are withheld rather than shown as zero.
 */
function FeedHealth({ status }: { status?: FeedStatus }) {
  const { t } = useT();
  const known = status !== undefined && happened(status.lastPolledAt);

  return (
    <FieldGroup label={t('settings.feeds.status')} hint={t('settings.feeds.lastPolledHint')}>
      <div className="flex flex-col gap-1.5 text-xs">
        <span className="text-carbon-textSub">
          {!status
            ? t('settings.feeds.statusUnknown')
            : status.polling
              ? t('settings.feeds.statusPolling')
              : t('settings.feeds.statusNotPolling')}
        </span>
        <span className="text-carbon-textMuted">
          {known ? `${t('settings.feeds.lastPolled')}: ${fmtWhen(status.lastPolledAt)}` : t('settings.feeds.lastPolledNever')}
        </span>
        {/* A row the server refuses to poll and a failed last look get
            different sentences, told apart by `polling`. */}
        {status?.error && (
          <span className="text-statusWarn">
            {status.polling ? t('settings.feeds.pollFailed') : t('settings.feeds.notPolledReason')}: {status.error}
          </span>
        )}
        {known && status && (
          <>
            <span className="text-carbon-textMuted">
              {status.seeded ? t('settings.feeds.seededYes') : t('settings.feeds.seededNo')}
            </span>
            <span className="text-carbon-textMuted">
              {t('settings.feeds.remembered', { n: status.remembered })}
            </span>
          </>
        )}
      </div>
    </FieldGroup>
  );
}

/**
 * fmtWhen formats an RFC3339 stamp in the reader's locale, or returns it raw
 * when it does not parse. It is absolute because the value is fetched once.
 */
function fmtWhen(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

/**
 * FeedProbe fetches the feed once and shows its entries, so a title filter can
 * be written against real titles. It stages and remembers nothing and works on
 * an unsaved row. A broken row throws with the server's sentence; a feed that
 * could not be read comes back as a result with `error` set.
 */
function FeedProbe({ url, filter }: { url: string; filter: string }) {
  const { t } = useT();
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<FeedTest | null>(null);
  const [refused, setRefused] = useState('');

  const run = async () => {
    setBusy(true);
    setRefused('');
    setResult(null);
    try {
      setResult(await testFeed(url, filter));
    } catch (e) {
      setRefused(String(e).replace(/^(Error|ApiError):\s*/, ''));
    } finally {
      setBusy(false);
    }
  };

  return (
    <FieldGroup label={t('settings.feeds.testResult')} hint={t('settings.feeds.testHint')}>
      <div className="flex flex-col gap-2">
        <Button className="w-fit" disabled={busy || !usableAddress(url)} onClick={() => void run()}>
          {busy ? t('settings.feeds.testBusy') : t('settings.feeds.test')}
        </Button>

        {refused && <p className="text-xs text-statusWarn">{refused}</p>}

        {result && (
          <div className="glim-well flex flex-col gap-2 p-3 text-xs">
            {result.error ? (
              <p className="text-statusWarn">
                {t('settings.feeds.testFailed')}: {result.error}
              </p>
            ) : (
              <>
                <p className="text-carbon-textSub">
                  {t('settings.feeds.testFeedName')}:{' '}
                  {result.title || <span className="text-carbon-textMuted">{t('settings.feeds.testNoName')}</span>}
                </p>
                {result.total === 0 ? (
                  <p className="text-statusWarn">{t('settings.feeds.testEmpty')}</p>
                ) : (
                  <>
                    <p className="text-carbon-textMuted">
                      {filter.trim() === ''
                        ? t('settings.feeds.testNoFilter', { total: result.total })
                        : t('settings.feeds.testMatched', { matched: result.matched, total: result.total })}
                    </p>
                    {filter.trim() !== '' && result.matched === 0 && (
                      <p className="text-statusWarn">{t('settings.feeds.testNoneMatch')}</p>
                    )}
                    <ul className="flex flex-col gap-1">
                      {result.entries.map((e) => (
                        <li key={e.link || e.title} className="flex items-baseline gap-2">
                          <span
                            className={`shrink-0 text-[11px] uppercase tracking-wider ${
                              e.matches ? 'text-carbon-textSub' : 'text-carbon-textMuted'
                            }`}
                          >
                            {e.matches ? t('settings.feeds.testMatchYes') : t('settings.feeds.testMatchNo')}
                          </span>
                          <span className={`min-w-0 truncate ${e.matches ? 'text-carbon-text' : 'text-carbon-textMuted'}`}>
                            {e.title}
                          </span>
                        </li>
                      ))}
                    </ul>
                    {result.total > result.entries.length && (
                      <p className="text-carbon-textMuted">
                        {t('settings.feeds.testShowing', { n: result.entries.length })}
                      </p>
                    )}
                  </>
                )}
              </>
            )}
            {/* Said at the result too: nothing was taken from the feed. */}
            <p className="text-carbon-textMuted">{t('settings.feeds.testSafe')}</p>
          </div>
        )}
      </div>
    </FieldGroup>
  );
}

/** Marker is one mark in the collapsed row, shown only when its field is set. */
function Marker({ icon, title }: { icon: ReactNode; title: string }) {
  // The house tooltip. The mark sits inside the expand button, so it drops the
  // tab stop, and its role would overwrite the img role that carries its name.
  const tip = useTooltip<HTMLSpanElement>(title);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <span role="img" aria-label={title} {...tipHoverProps} className="hidden shrink-0 text-carbon-textMuted sm:block">
        {icon}
      </span>
      {tip.node}
    </>
  );
}
