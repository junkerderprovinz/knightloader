import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import {
  Button,
  FieldError,
  FieldGroup,
  IconBadge,
  NumberInput,
  TextInput,
  ToggleRow,
  useTooltip,
  type ButtonVerdict,
} from '../../../components/ui';
import { PathInput } from '../../../components/FolderPicker';
import { Tabs } from '../../../components/Tabs';
import { TestButton } from '../../../components/TestButton';
import {
  IconAdd,
  IconBook,
  IconCheck,
  IconFileAudio,
  IconFilePlaylist,
  IconFileVideo,
  IconFilter,
  IconFolder,
  IconLabelText,
  IconPlay,
  IconPriority,
  IconReject,
  IconRestore,
  IconRetry,
  IconSchedules,
  IconSearch,
  IconShieldOn,
  IconTrash,
} from '../../../lib/icons';
import { fmtDate, fmtPlaytime } from '../../../lib/format';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  fetchFeeds,
  priorityChoices,
  resolveFeed,
  testFeed,
  type FeedKind,
  type FeedResolved,
  type FeedSkipReason,
  type FeedStatus,
  type FeedSubscription,
  type FeedTest,
  type PriorityChoice,
} from '../../../lib/api';
import { happened } from '../../../lib/countdown';
import { useDraft } from '../context';
import { RowRefusal, SettingRow, Sheet, SubCard } from '../controls';
import { ModuleCard } from '../ModuleToggle';
import { BackfillRow, FIRST_BACKFILL, LengthFields, MediathekFields, PatternField } from './FeedEntries';
import { MediaRows, SponsorBlockOverride } from './FeedMedia';

// Feeds lists the subscriptions this instance follows: RSS and Atom feeds,
// yt-dlp channels and playlists, and MediathekViewWeb searches, each fetched
// on its own timer, with new entries handed to the collector like a pasted
// link.
//
// The address is the identity: the poller and the record of added entries are
// keyed on it, so a stored row's address and kind are read only and changing
// them means removing the row and adding another. The health comes from GET
// /api/feeds, kept in memory, so after a restart a feed reads as not checked
// yet. The test only reads. A new row stays out of the draft until its
// address is valid, and every field the server would refuse is held back,
// because the server refuses the whole settings document over one bad row. An
// interval of 0 means the server's quarter of an hour, and an absent priority
// differs from 0.

/** feed.DefaultIntervalMinutes, what a stored 0 resolves to. */
const DEFAULT_INTERVAL_MINUTES = 15;
/** The range feed.Sanitize pulls a non-zero interval into. */
const MIN_INTERVAL_MINUTES = 1;
const MAX_INTERVAL_MINUTES = 10080;

/** The priority tab that removes the key rather than setting a value. */
const NO_PRIORITY = 'unset';

const KINDS: FeedKind[] = ['rss', 'channel', 'mediathek'];

const KIND_WORDS: Record<FeedKind, { kind: TranslationKey; url: TranslationKey; hint: TranslationKey; placeholder: string }> = {
  rss: {
    kind: 'settings.feeds.kindRss',
    url: 'settings.feeds.url',
    hint: 'settings.feeds.urlHint',
    placeholder: 'https://example.org/feed.xml',
  },
  channel: {
    kind: 'settings.feeds.kindChannel',
    url: 'settings.feeds.channelUrl',
    hint: 'settings.feeds.channelUrlHint',
    placeholder: 'https://www.youtube.com/@handle',
  },
  mediathek: {
    kind: 'settings.feeds.kindMediathek',
    url: 'settings.feeds.searchUrl',
    hint: 'settings.feeds.searchUrlHint',
    placeholder: 'https://mediathekviewweb.de/#query=',
  },
};

const REASON_WORDS: Record<FeedSkipReason, TranslationKey> = {
  include: 'settings.feeds.reasonInclude',
  exclude: 'settings.feeds.reasonExclude',
  short: 'settings.feeds.reasonShort',
  long: 'settings.feeds.reasonLong',
  downloaded: 'settings.feeds.reasonDownloaded',
};

const kindOf = (row: FeedSubscription): FeedKind => row.kind || 'rss';

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

/** clampInterval keeps 0 and cuts anything else into the range feed.Sanitize uses. */
function clampInterval(v: number): number {
  if (!Number.isFinite(v)) return 0;
  const whole = Math.round(v);
  if (whole === 0) return 0;
  return Math.min(Math.max(whole, MIN_INTERVAL_MINUTES), MAX_INTERVAL_MINUTES);
}

/**
 * without removes optional keys from a row. Absent and empty mean the same for
 * most of them, but not for the priority, where 0 is a value.
 */
function without(row: FeedSubscription, ...keys: (keyof FeedSubscription)[]): FeedSubscription {
  const next = { ...row };
  for (const key of keys) delete next[key];
  return next;
}

/** A switch's value, with off written as an absent key like every other "no opinion". */
function flag(row: FeedSubscription, key: 'autoStart' | 'allowDownloaded', on: boolean): FeedSubscription {
  return on ? { ...row, [key]: true } : without(row, key);
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

  // feed.Sanitize answers nil for an empty list.
  const rows = cfg.feeds ?? [];
  const priorities = usePriorityTabs();

  const [openRow, setOpenRow] = useState('');
  const [pending, setPending] = useState<PendingRow[]>([]);
  // A row keeps the React key it was typed under once it is stored, so the
  // window it is edited in stays open; that key is kept by the row's address.
  const [rowIds, setRowIds] = useState<Record<string, string>>({});
  const idOf = (url: string) => rowIds[url] ?? storedId(url);

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

  /** What the server would make of a pending row's address: refuse it, merge it away, or take it. */
  const check = (row: FeedSubscription): Verdict => {
    const url = row.url.trim();
    if (url === '') return 'blank';
    if (!usableAddress(url)) return 'invalid';
    // A second row for one address would be merged away on save.
    if (rows.some((r) => r.url.trim() === url)) return 'duplicate';
    return 'ok';
  };

  /** Moves a pending row into the draft once check lets it. */
  const commit = (row: FeedSubscription): Verdict => {
    const verdict = check(row);
    if (verdict === 'ok') write([...rows, { ...row, url: row.url.trim() }]);
    return verdict;
  };

  const add = () => {
    const row: PendingRow = { id: freshId(), row: { url: '', intervalMinutes: 0 } };
    setPending((p) => [...p, row]);
    setOpenRow(row.id);
  };

  const rowCount = rows.length + pending.length;

  return (
    // Switched off, the rows are parked on the server and the card folds, so
    // Add cannot put a row in their place.
    <ModuleCard
      id="feeds"
      hue={hue}
      className="flex flex-col gap-4"
      titleHint={t('settings.feeds.titleHint')}
      titleRight={
        <Button kind="secondary" icon={<IconAdd width={16} height={16} />} onClick={add}>
          {t('settings.feeds.add')}
        </Button>
      }
    >
      {rowCount === 0 ? (
        // Inside the card rather than an EmptyState, which would hide Add.
        <p className="py-6 text-center text-sm text-carbon-textSub">
          {t('settings.feeds.empty')}
          <span className="mt-1 block text-meta text-carbon-textMuted">{t('settings.feeds.emptyHint')}</span>
        </p>
      ) : (
        <ul className="flex flex-col">
          {/* One list, so a row that gets stored keeps its place among the
              keys React compares. */}
          {[
            ...rows.map((row, i) => (
              <FeedRow
                key={idOf(row.url)}
                row={row}
                index={i}
                last={i === rowCount - 1}
                stored
                priorities={priorities}
                status={health[row.url]}
                open={openRow === idOf(row.url)}
                onToggle={() => setOpenRow(openRow === idOf(row.url) ? '' : idOf(row.url))}
                onCheck={() => 'ok'}
                onCommit={() => 'ok'}
                onChange={(next) => write(rows.map((r) => (r.url === row.url ? next : r)))}
                onRemove={() => write(rows.filter((r) => r.url !== row.url))}
              />
            )),
            ...pending.map((p, i) => (
              <FeedRow
                key={p.id}
                row={p.row}
                index={rows.length + i}
                last={rows.length + i === rowCount - 1}
                stored={false}
                priorities={priorities}
                open={openRow === p.id}
                onToggle={() => setOpenRow(openRow === p.id ? '' : p.id)}
                onCheck={check}
                onCommit={(row) => {
                  // Kept so closing a half-typed row keeps the text.
                  setPending((list) => list.map((r) => (r.id === p.id ? { ...r, row } : r)));
                  const verdict = commit(row);
                  if (verdict === 'ok') {
                    setPending((list) => list.filter((r) => r.id !== p.id));
                    setRowIds((ids) => ({ ...ids, [row.url.trim()]: p.id }));
                  }
                  return verdict;
                }}
                onChange={(next) => setPending((list) => list.map((r) => (r.id === p.id ? { ...r, row: next } : r)))}
                onRemove={() => setPending((list) => list.filter((r) => r.id !== p.id))}
              />
            )),
          ]}
        </ul>
      )}
    </ModuleCard>
  );
}

/** The marks a collapsed row carries, one per field it sets, in the order the window shows them. */
function rowMarks(row: FeedSubscription, t: (key: TranslationKey) => string): { icon: ReactNode; title: string }[] {
  const glyph = { width: 14, height: 14 };
  const kind = kindOf(row);
  const marks: { icon: ReactNode; title: string; on: unknown }[] = [
    { icon: <IconFilePlaylist {...glyph} />, title: t(KIND_WORDS.channel.kind), on: kind === 'channel' },
    { icon: <IconSearch {...glyph} />, title: t(KIND_WORDS.mediathek.kind), on: kind === 'mediathek' },
    { icon: <IconRestore {...glyph} />, title: t('settings.feeds.backfill'), on: kind === 'channel' && row.backfill },
    { icon: <IconFilter {...glyph} />, title: t('settings.feeds.filter'), on: row.titleFilter },
    { icon: <IconReject {...glyph} />, title: t('settings.feeds.exclude'), on: row.excludeFilter },
    { icon: <IconSchedules {...glyph} />, title: t('settings.feeds.length'), on: row.minMinutes || row.maxMinutes },
    { icon: <IconRetry {...glyph} />, title: t('settings.feeds.allowDownloaded'), on: row.allowDownloaded },
    { icon: <IconPlay {...glyph} />, title: t('settings.feeds.autoStart'), on: row.autoStart },
    { icon: <IconFolder {...glyph} />, title: t('settings.feeds.dir'), on: row.dir },
    { icon: <IconPriority {...glyph} />, title: t('settings.feeds.priority'), on: row.priority !== undefined },
    { icon: <IconLabelText {...glyph} />, title: t('settings.feeds.naming'), on: row.naming },
    { icon: <IconBook {...glyph} />, title: t('settings.feeds.library'), on: row.library },
    {
      icon: row.media?.variants.length === 1 && row.media.variants[0] === 'audio' ? <IconFileAudio {...glyph} /> : <IconFileVideo {...glyph} />,
      title: t('settings.feeds.media'),
      on: kind !== 'mediathek' && row.media,
    },
    { icon: <IconShieldOn {...glyph} />, title: t('settings.resolvers.sponsorBlock'), on: kind !== 'mediathek' && row.sponsorBlock },
  ];
  return marks.filter((m) => m.on);
}

/**
 * FeedRow shows one subscription as its name or address and the fields it
 * sets, and all of them in the window the row opens. A new row's address
 * reaches the draft only once it is valid.
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
  onCheck,
  onCommit,
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
  onCheck: (row: FeedSubscription) => Verdict;
  onCommit: (row: FeedSubscription) => Verdict;
  onChange: (next: FeedSubscription) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  const kind = kindOf(row);
  const words = KIND_WORDS[kind];
  const [text, setText] = useState(row.url);
  const [verdict, setVerdict] = useState<Verdict>('blank');
  const urlReasonId = useId();

  // A pasted address is looked up before it is stored: what was pasted may be
  // a channel page or a search rather than a feed.
  const pasted = useRef(false);
  const [lookingUp, setLookingUp] = useState(false);
  const [found, setFound] = useState<FeedResolved | null>(null);
  const [notFound, setNotFound] = useState('');

  // A new channel waits until its older videos are chosen: the server checks
  // a new subscription as soon as it is saved, and only that first check
  // hands older videos over.
  const held = !stored && kind === 'channel';

  const urlReason =
    verdict === 'invalid'
      ? t('settings.feeds.urlInvalid')
      : verdict === 'duplicate'
        ? t('settings.feeds.duplicate')
        : notFound;

  const take = (resolved: FeedResolved, typed: string) => {
    const { channel: _channel, ...fields } = resolved;
    const next = { ...row, ...fields, url: fields.url || typed };
    setFound(null);
    setText(next.url);
    if (kindOf(next) === 'channel') onChange({ ...next, backfill: next.backfill ?? { ...FIRST_BACKFILL } });
    else setVerdict(onCommit(next));
  };

  const lookUp = async (typed: string) => {
    if (!usableAddress(typed)) return;
    setLookingUp(true);
    try {
      const answer = await resolveFeed(typed);
      // A channel and a search are offered, since each has a choice to make;
      // a plain feed has none and is taken.
      if (answer.kind === 'mediathek' || answer.channel) setFound({ ...answer, url: answer.url || typed });
      else take(answer, typed);
    } catch (e) {
      setNotFound(e instanceof Error && e.message ? e.message : String(e));
    } finally {
      setLookingUp(false);
    }
  };

  const commitUrl = () => {
    if (lookingUp || found) return;
    const typed = text.trim();
    const search = row.mediathek;
    // A search with no names yet is read from its address.
    const unnamed = !(search && (search.channel || search.topic || search.title));
    if (kind === 'mediathek' && unnamed && usableAddress(typed)) {
      void lookUp(typed);
      return;
    }
    setVerdict((held ? onCheck : onCommit)({ ...row, url: typed }));
  };

  const chooseKind = (next: FeedKind) => {
    const out = without(row, 'kind', 'backfill', 'mediathek');
    if (next !== 'rss') out.kind = next;
    if (next === 'channel') out.backfill = row.backfill ?? { ...FIRST_BACKFILL };
    if (next === 'mediathek' && row.mediathek) out.mediathek = row.mediathek;
    onChange(out);
  };

  // A stored 0 runs at the server's quarter of an hour, shown quieter than a
  // typed number.
  const derived = row.intervalMinutes === 0;
  const effective = derived ? DEFAULT_INTERVAL_MINUTES : row.intervalMinutes;
  const name = row.title?.trim() || row.url;

  // The house tooltip rather than a native title. Both spans sit inside the
  // row's button, so they take no role and no tab stop of their own.
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
          aria-haspopup="dialog"
          className="flex min-w-0 items-center gap-3 text-start"
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          {/* Truncated, with the whole address in the tooltip. */}
          <span dir={row.title ? 'auto' : 'ltr'} {...urlTipProps} className="min-w-0 flex-1 truncate text-sm text-carbon-text">
            {name || <span className="text-carbon-textMuted">{t(words.url)}</span>}
          </span>
          {rowMarks(row, t).map((m) => (
            <Marker key={m.title} icon={m.icon} title={m.title} />
          ))}
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
      {stored && <RowRefusal field={`feeds.${index}`} />}

      {open && (
        <Sheet title={name || t(words.url)} hue={index} widest onClose={onToggle}>
          <div className="flex flex-col gap-4">
            {/* Read only once stored, since the kind and the address are the
                identity; a different one is a new subscription. */}
            <SettingRow label={t('settings.feeds.kind')} hint={t('settings.feeds.kindHint')}>
              {stored ? (
                <span className="text-sm text-carbon-textSub">{t(words.kind)}</span>
              ) : (
                <Tabs
                  variant="well"
                  size="sm"
                  inline
                  hug
                  label={t('settings.feeds.kind')}
                  active={kind}
                  onSelect={(id) => chooseKind(id as FeedKind)}
                  items={KINDS.map((k) => ({ id: k, label: t(KIND_WORDS[k].kind) }))}
                />
              )}
            </SettingRow>

            <SettingRow control="long" label={t(words.url)} hint={t(words.hint)}>
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
                  placeholder={words.placeholder}
                  // Marked, not corrected: the text stays as typed.
                  aria-invalid={urlReason !== ''}
                  aria-describedby={urlReason ? urlReasonId : undefined}
                  aria-busy={lookingUp}
                  onPaste={() => {
                    pasted.current = true;
                  }}
                  onChange={(e) => {
                    const typed = e.target.value;
                    setText(typed);
                    setVerdict('blank');
                    setFound(null);
                    setNotFound('');
                    if (pasted.current) {
                      pasted.current = false;
                      void lookUp(typed.trim());
                    }
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
              {urlReason && <FieldError id={urlReasonId}>{urlReason}</FieldError>}
            </SettingRow>

            {found && <FoundOffer found={found} onTake={(r) => take(r, text.trim())} />}

            {held && (
              <>
                <BackfillRow row={row} onChange={onChange} />
                <div className="flex justify-end">
                  <Button icon={<IconAdd />} onClick={() => setVerdict(onCommit({ ...row, url: text.trim() }))}>
                    {t('settings.feeds.add')}
                  </Button>
                </div>
              </>
            )}

            <SettingRow control="long" label={t('settings.feeds.name')} hint={t('settings.feeds.nameHint')}>
              <TextInput
                value={row.title ?? ''}
                onChange={(e) =>
                  onChange(e.target.value === '' ? without(row, 'title') : { ...row, title: e.target.value })
                }
              />
            </SettingRow>

            {/* 0 takes the server's quarter of an hour; other values are cut
                into 1..10080 as feed.Sanitize does. */}
            <SettingRow control="short" label={t('settings.feeds.interval')} hint={t('settings.feeds.intervalHint')}>
              <NumberInput
                value={row.intervalMinutes}
                min={0}
                max={MAX_INTERVAL_MINUTES}
                step={1}
                onValue={(v) => onChange({ ...row, intervalMinutes: clampInterval(v) })}
              />
            </SettingRow>
          </div>

          {kind === 'mediathek' && (
            <SubCard label={t('settings.feeds.sectionMediathek')} hint={t('settings.feeds.sectionMediathekHint')}>
              <MediathekFields row={row} onChange={onChange} />
            </SubCard>
          )}

          <SubCard label={t('settings.feeds.sectionEntries')}>
            {kind === 'channel' && !held && <BackfillRow row={row} onChange={onChange} />}
            <PatternField
              label={t('settings.feeds.filter')}
              hint={t('settings.feeds.filterHint')}
              value={row.titleFilter}
              onValue={(v) => onChange(v === undefined ? without(row, 'titleFilter') : { ...row, titleFilter: v })}
            />
            <PatternField
              label={t('settings.feeds.exclude')}
              hint={t('settings.feeds.excludeHint')}
              value={row.excludeFilter}
              onValue={(v) => onChange(v === undefined ? without(row, 'excludeFilter') : { ...row, excludeFilter: v })}
            />
            <LengthFields row={row} onChange={onChange} />
            <ToggleRow
              checked={row.allowDownloaded === true}
              onChange={(v) => onChange(flag(row, 'allowDownloaded', v))}
              label={t('settings.feeds.allowDownloaded')}
              hint={t('settings.feeds.allowDownloadedHint')}
            />
          </SubCard>

          <SubCard label={t('settings.feeds.sectionDownloads')}>
            {/* Off by default, since anyone who can publish to the feed then
                decides what this machine downloads. */}
            <ToggleRow
              checked={row.autoStart === true}
              onChange={(v) => onChange(flag(row, 'autoStart', v))}
              label={t('settings.feeds.autoStart')}
              hint={t('settings.feeds.autoStartHint')}
            />

            {/* PathInput, since the folder may be a template. It is checked only
                when an entry arrives, and a failure there drops the priority too,
                since both are written in one call. */}
            <SettingRow control="long" label={t('settings.feeds.dir')} hint={t('settings.feeds.dirHint')}>
              <PathInput
                value={row.dir ?? ''}
                title={t('settings.feeds.dir')}
                onValue={(dir) => onChange(dir.trim() === '' ? without(row, 'dir') : { ...row, dir })}
              />
            </SettingRow>

            <SettingRow label={t('settings.feeds.naming')} hint={t('settings.feeds.namingHint')}>
              <Tabs
                variant="well"
                size="sm"
                inline
                hug
                label={t('settings.feeds.naming')}
                active={row.naming || 'entry'}
                onSelect={(id) => onChange(id === 'channel' ? { ...row, naming: 'channel' } : without(row, 'naming'))}
                items={[
                  { id: 'entry', label: t('settings.feeds.namingEntry') },
                  { id: 'channel', label: t('settings.feeds.namingChannel') },
                ]}
              />
            </SettingRow>

            <SettingRow label={t('settings.feeds.library')} hint={t('settings.feeds.libraryHint')}>
              <Tabs
                variant="well"
                size="sm"
                inline
                hug
                label={t('settings.feeds.library')}
                active={row.library || 'standard'}
                onSelect={(id) => onChange(id === 'series' ? { ...row, library: 'series' } : without(row, 'library'))}
                items={[
                  { id: 'standard', label: t('settings.feeds.libraryStandard') },
                  { id: 'series', label: t('settings.feeds.librarySeries') },
                ]}
              />
            </SettingRow>

            {priorities.length > 0 && (
              <SettingRow label={t('settings.feeds.priority')} hint={t('settings.feeds.priorityHint')}>
                <Tabs
                  variant="well"
                  size="sm"
                  inline
                  hug
                  label={t('settings.feeds.priority')}
                  // Matched on undefined, since 0 is a real priority.
                  active={row.priority === undefined ? NO_PRIORITY : String(row.priority)}
                  onSelect={(id) =>
                    onChange(id === NO_PRIORITY ? without(row, 'priority') : { ...row, priority: Number(id) })
                  }
                  items={priorities}
                />
              </SettingRow>
            )}
          </SubCard>

          {/* A Mediathek search stages direct links, which yt-dlp never sees. */}
          {kind !== 'mediathek' && (
            <SubCard label={t('settings.feeds.sectionYtdlp')} hint={t('settings.feeds.sectionYtdlpHint')}>
              <MediaRows row={row} onChange={onChange} />
              <SponsorBlockOverride row={row} onChange={onChange} />
            </SubCard>
          )}

          {stored && <FeedHealth status={status} />}
          <FeedProbe row={stored ? row : { ...row, url: text.trim() }} />
        </Sheet>
      )}
    </li>
  );
}

/**
 * FoundOffer is what a pasted address turned out to be, with the choice it
 * leaves: a channel as its feed of new videos or as a channel with its older
 * ones, a Mediathek search as a subscription.
 */
function FoundOffer({ found, onTake }: { found: FeedResolved; onTake: (row: Partial<FeedSubscription>) => void }) {
  const { t } = useT();
  const { channel, ...feed } = found;
  return (
    <div className="glim-well flex flex-col gap-3 p-3 text-sm">
      <p dir="auto" className="text-carbon-textSub">
        {found.kind === 'mediathek'
          ? t('settings.feeds.foundSearch', { title: found.title || found.url || '' })
          : t('settings.feeds.foundChannel', { title: found.title || found.url || '' })}
      </p>
      <div className="flex flex-wrap justify-end gap-2">
        {found.kind === 'mediathek' ? (
          <Button icon={<IconAdd />} onClick={() => onTake(feed)}>
            {t('settings.feeds.subscribeSearch')}
          </Button>
        ) : (
          <>
            {channel && (
              <Button kind="secondary" icon={<IconRestore />} onClick={() => onTake(channel)}>
                {t('settings.feeds.subscribeOlder')}
              </Button>
            )}
            <Button icon={<IconAdd />} onClick={() => onTake(feed)}>
              {t('settings.feeds.subscribeNew')}
            </Button>
          </>
        )}
      </div>
    </div>
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
  return Number.isNaN(d.getTime()) ? iso : fmtDate(iso);
}

/**
 * FeedProbe fetches the subscription once and shows its entries, so the
 * filters can be written against real titles and lengths. It stages and
 * remembers nothing and works on an unsaved row. A broken row throws with the
 * server's sentence; a feed that could not be read comes back as a result with
 * `error` set.
 */
function FeedProbe({ row }: { row: FeedSubscription }) {
  const { t } = useT();
  const [result, setResult] = useState<FeedTest | null>(null);
  const [refused, setRefused] = useState('');

  const run = async (): Promise<ButtonVerdict> => {
    setRefused('');
    setResult(null);
    try {
      const found = await testFeed(row);
      setResult(found);
      return found.error ? 'fail' : 'ok';
    } catch (e) {
      setRefused(String(e).replace(/^(Error|ApiError):\s*/, ''));
      return 'fail';
    }
  };

  // The server also turns entries away for reasons the row does not set, such
  // as an entry downloaded before, so the summary follows the answer as well.
  const otherReason = !!result?.entries.some((e) => e.reason && e.reason !== 'include');
  const titleOnly = !!row.titleFilter && !row.excludeFilter && !row.minMinutes && !row.maxMinutes && !otherReason;
  const filtered =
    !!(row.titleFilter || row.excludeFilter || row.minMinutes || row.maxMinutes) ||
    otherReason ||
    (!!result && result.matched < result.total);

  return (
    <FieldGroup label={t('settings.feeds.testResult')} hint={t('settings.feeds.testHint')}>
      <div className="flex flex-col gap-2">
        {/* Why the row could not be read at all, above the button until the next try. */}
        {refused && <p dir="auto" className="text-xs text-carbon-textSub">{refused}</p>}

        <div className="flex">
          <TestButton
            icon={<IconCheck />}
            label={t('settings.feeds.test')}
            busyLabel={t('settings.feeds.testBusy')}
            words={{ ok: t('test.passed'), fail: t('test.failed') }}
            disabled={!usableAddress(row.url)}
            run={run}
            resetKey={JSON.stringify(row)}
          />
        </div>

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
                      {!filtered
                        ? t('settings.feeds.testNoFilter', { total: result.total })
                        : titleOnly
                          ? t('settings.feeds.testMatched', { matched: result.matched, total: result.total })
                          : t('settings.feeds.testTaken', { matched: result.matched, total: result.total })}
                    </p>
                    {filtered && result.matched === 0 && (
                      <p className="text-statusWarn">
                        {titleOnly ? t('settings.feeds.testNoneMatch') : t('settings.feeds.testNoneTaken')}
                      </p>
                    )}
                    <ul className="flex flex-col gap-1">
                      {result.entries.map((e) => (
                        <li key={e.link || e.title} className="flex items-baseline gap-2">
                          <span
                            className={`shrink-0 text-meta uppercase tracking-wider ${
                              e.matches ? 'text-carbon-textSub' : 'text-carbon-textMuted'
                            }`}
                          >
                            {e.matches
                              ? t('settings.feeds.testMatchYes')
                              : e.reason && REASON_WORDS[e.reason]
                                ? t(REASON_WORDS[e.reason])
                                : t('settings.feeds.testMatchNo')}
                          </span>
                          <span className={`min-w-0 flex-1 truncate ${e.matches ? 'text-carbon-text' : 'text-carbon-textMuted'}`}>
                            {e.title}
                          </span>
                          {/* 0 means the feed did not say. */}
                          {!!e.duration && (
                            <span className="glim-num shrink-0 text-carbon-textMuted">{fmtPlaytime(e.duration)}</span>
                          )}
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
          </div>
        )}
      </div>
    </FieldGroup>
  );
}

/** Marker is one mark on the row, shown only when its field is set. */
function Marker({ icon, title }: { icon: ReactNode; title: string }) {
  // The house tooltip. The mark sits inside the row's button, so it drops the
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
