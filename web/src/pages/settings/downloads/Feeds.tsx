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
import { useDraft, useFeatures } from '../context';

/**
 * The RSS and Atom subscriptions this instance follows: one row per feed, each
 * fetched on its own timer, each new entry handed to the collector exactly like
 * a pasted link.
 *
 * Five things about this card are decisions rather than layout.
 *
 * THE ADDRESS IS THE IDENTITY, so a stored row's address box is read only. The
 * poller and the record of which entries have already been added are both keyed
 * on the address alone, so editing it in place does not correct a subscription:
 * it retires one and starts another that has forgotten everything the first one
 * knew. There is no server route to hand that memory over, and none to clear it
 * either, so a box that took the edit on a blur would be a one-keystroke way to
 * either re-read a feed from scratch or, worse, leave a record behind that
 * nothing ever sweeps. Pointing at a different address is therefore the two
 * deliberate steps it really is: remove the row, add a subscription. Rows that
 * predate this card can still be rewritten wholesale through the raw list row on
 * the Advanced page, which is the escape hatch for everything this card will not
 * express.
 *
 * THE HEALTH IS THE SERVER'S, NOT A GUESS. When a subscription was last
 * fetched, whether that fetch failed and how much it remembers come from
 * GET /api/feeds, which is a table the poller keeps in memory beside itself and
 * deliberately not part of the settings document: it is diagnostic, it is
 * blanked by a restart, and it has no business in a file that is read back and
 * diffed. The one thing that follows from that, and the one thing easy to draw
 * wrongly: an absent lastPolledAt means "nothing to report YET", not "never
 * checked". The subscription's own memory of what it has added survives a
 * restart while this table does not, so a feed followed for months reads as
 * unchecked for the few seconds after the server comes back, and drawing that
 * as a problem would be a false alarm on every boot.
 *
 * THE TEST ONLY READS, and says so where it is pressed. It fetches the address
 * once, reports the feed's name and its first entries, and stages nothing and
 * remembers nothing. That is what makes it usable on an address nobody has
 * saved, which is most of its value: a title filter is a regular expression
 * typed blind against titles nobody has seen, and this is the only way to see
 * them.
 *
 * THE AUTOSAVE TRAP is the reason a half-typed address never reaches the draft.
 * The settings shell saves 600 ms after any draft change and the server refuses
 * the WHOLE settings PATCH when one feed row will not validate, naming the row
 * number. Typing "https" into a new row would therefore fire a save, be refused,
 * and take every unrelated edit made on every other settings page down with it.
 * A new row is held in local state until its address is one the server will
 * accept, and the title filter is committed only once it compiles here first.
 *
 * ZERO IS "NO OPINION" ON THE INTERVAL, never "off" and never "as fast as
 * possible". It resolves to the server's own quarter of an hour, and it is kept
 * and shown as 0 rather than quietly rewritten to 15, because a field that
 * writes back a number nobody typed is a field nobody can read. A non-zero value
 * outside 1..10080 is CLAMPED by the server without a word, so it is clamped
 * here too: the alternative is a box that keeps showing 20000 after a save that
 * stored a week.
 *
 * PRIORITY HAS AN EIGHTH ITEM, and it is not the ladder's own "Default". Absent
 * and 0 are different values on the wire (the Go side holds a *int): 0 is a real
 * priority the subscription asks for, absent is the subscription saying nothing.
 * The neutral item removes the key from the row entirely.
 */

/** feed.DefaultIntervalMinutes: what a stored 0 resolves to when the poller runs. */
const DEFAULT_INTERVAL_MINUTES = 15;
/** feed.Sanitize pulls any NON-ZERO interval into this range. A minute is the
 *  floor because every fetch is a request to somebody else's server, and the
 *  week is a ceiling because the value becomes a timer duration. */
const MIN_INTERVAL_MINUTES = 1;
const MAX_INTERVAL_MINUTES = 10080;

/** The priority tab that removes the key rather than setting a value. */
const NO_PRIORITY = 'unset';

/**
 * Whether the server will take this address, checked with the same three
 * questions feed.Validate asks: is it readable, is the scheme http or https,
 * does it name a host.
 *
 * A local copy of a server-side rule is normally a liability, but this one earns
 * itself: the refusal it stands in for does not refuse this row, it refuses the
 * entire settings document, including whatever somebody was editing two pages
 * away. JavaScript's URL parser is not Go's, so this is deliberately the loose
 * half of the check and the server stays the authority.
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
 * Whether a title filter is obviously broken.
 *
 * JavaScript's RegExp is not Go's RE2: it accepts patterns Go refuses
 * (backreferences, lookahead) and refuses none that Go accepts. So this only
 * ever catches the unclosed bracket, and a pattern that passes here can still be
 * refused at save. That is the right way round: it never blocks something the
 * server would have taken.
 */
function compiles(pattern: string): boolean {
  try {
    new RegExp(pattern);
    return true;
  } catch {
    return false;
  }
}

/**
 * The interval as the server would store it. 0 is passed through untouched
 * because it is a value and not a blank, everything else lands inside the range
 * feed.Sanitize would silently pull it into anyway.
 */
function clampInterval(v: number): number {
  if (!Number.isFinite(v)) return 0;
  const whole = Math.round(v);
  if (whole === 0) return 0;
  return Math.min(Math.max(whole, MIN_INTERVAL_MINUTES), MAX_INTERVAL_MINUTES);
}

/**
 * A row with one of the three optional keys GONE, rather than present and empty.
 *
 * Absent and empty are the same thing for the folder and the filter, but not for
 * the priority, where absent is "say nothing" and 0 is a priority. One helper for
 * all three so the priority case cannot be the one that gets it wrong.
 */
function without(row: FeedSubscription, key: 'titleFilter' | 'dir' | 'priority'): FeedSubscription {
  const next = { ...row };
  delete next[key];
  return next;
}

/** What committing a typed address did, so the row can say why nothing happened. */
type Verdict = 'ok' | 'blank' | 'invalid' | 'duplicate';

/**
 * A row that has been added but has no usable address yet, and therefore nothing
 * the draft can be keyed on. It stays here until it earns one: a row whose
 * address is blank is DELETED by feed.Sanitize, which is exactly what an
 * untouched Add button produces, so putting it in the draft early would autosave
 * it, get it deleted, and make it vanish under the cursor.
 */
interface PendingRow {
  id: string;
  row: FeedSubscription;
}

let pendingCounter = 0;
const freshId = () => `p${(pendingCounter++).toString(36)}`;

/**
 * Row identity for React and for "which row is open".
 *
 * A stored row is identified by its address and a new one by its client-side id,
 * and the two namespaces are kept apart by a prefix rather than trusted to
 * differ: an address is whatever somebody types. Never the array index, or focus
 * jumps between rows when the applied document comes back with a duplicate
 * collapsed out of it.
 */
const storedId = (url: string) => `u:${url}`;

/**
 * The seven queue priorities as the server offers them, highest first, behind
 * the eighth item that means the subscription names none.
 *
 * Built from /api/queue/priorities rather than from a ladder written out here,
 * so this strip cannot disagree with the one on the add-links form about how
 * many priorities the app has. The strip stays out entirely while the ladder has
 * not arrived: a row of guessed steps is worse than a row of none.
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
    // Its own word rather than the rule editor's "Unchanged": that one is right
    // for a rule, which edits a link that already has a priority, and wrong
    // here, where a brand-new subscription has nothing to leave unchanged. The
    // field's own hint already tells people to "leave it on Not set", so this
    // is the label that sentence was written against.
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

  // null and never an empty array on the wire: feed.Sanitize answers nil for an
  // empty list, which is the state a fresh install is in.
  const rows = cfg.feeds ?? [];
  const priorities = usePriorityTabs();

  // Switched off on the Modules page, which CLEARED settings.feeds and stored the
  // rows server-side, so anything typed here now would be typed into a list the
  // server is not reading. Tested on parked as well as enabled, never on enabled
  // alone: an empty list on a fresh install also reads as off, and locking for
  // that reason would leave nowhere to type the first address.
  const feedsModule = features.modules.find((m) => m.id === 'feeds');
  const parked = feedsModule !== undefined && !feedsModule.enabled && feedsModule.parked;

  const [openRow, setOpenRow] = useState('');
  const [pending, setPending] = useState<PendingRow[]>([]);

  /**
   * The health table, keyed by address exactly as the server keys it.
   *
   * Fetched once on mount and NOT polled. It is a diagnostic read, the numbers
   * in it change on the subscription's own timer (a quarter of an hour by
   * default), and a card that re-fetched every few seconds would be asking a
   * question whose answer cannot have changed, forever, on a settings page
   * somebody has left open in a background tab.
   *
   * A failure leaves the map empty rather than showing anything: every row then
   * reads as "nothing to report yet", which is exactly what is true.
   */
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
        /* No status is drawn, which is the honest state when nothing answered. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  // Never patch({ feeds: undefined }), which the diff in the settings shell would
  // send as a changed key with no value. An emptied list goes out as [] and comes
  // back as null, which is the same thing said the server's way.
  const write = (next: FeedSubscription[]) => patch({ feeds: next });

  /**
   * Move a pending row into the draft under the address that was typed. Refuses
   * rather than writing whenever the server would refuse the whole document or
   * quietly merge the row away.
   */
  const commit = (typed: string, row: FeedSubscription): Verdict => {
    const url = typed.trim();
    if (url === '') return 'blank';
    if (!usableAddress(url)) return 'invalid';
    // Two rows naming one address are collapsed into the first when the document
    // is saved, so the second one would look configured and poll nothing.
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
            {/* The module's state, not an explanation of it: the list below is
                dimmed, and this says which of the two reasons a list can be
                empty this one is. */}
            {parked && <LabelBadge label={t('settings.modules.off')} />}
            <Button icon={<IconPlus width={16} height={16} />} disabled={parked} onClick={add}>
              {t('settings.feeds.add')}
            </Button>
          </div>
        }
      >
        {t('settings.feeds.title')}
      </SectionTitle>

      {/* Dimmed and inert rather than hidden while the module is parked: a card
          that vanishes teaches nobody that the feature exists, and the switch
          that brings the stored subscriptions back is one page away. */}
      <div className={parked ? 'pointer-events-none opacity-40' : ''}>
        {rowCount === 0 ? (
          // Inside the card rather than instead of it: the Add button above is
          // the only way out of this state, and swapping the card for an
          // EmptyState would take it off the page.
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
                  // Kept even when it cannot be stored yet, so collapsing a row
                  // whose address is still half typed does not throw it away.
                  setPending((list) =>
                    list.map((r) => (r.id === p.id ? { ...r, row: { ...r.row, url: typed } } : r)),
                  );
                  const verdict = commit(typed, p.row);
                  if (verdict === 'ok') {
                    setPending((list) => list.filter((r) => r.id !== p.id));
                    // The row is keyed by its address once it is stored, so it
                    // remounts here; without this the editor would close on the
                    // person who just finished typing into it.
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
      </div>
    </Card>
  );
}

/**
 * One subscription, collapsed to its address and what it overrides, expanded to
 * the five fields.
 *
 * The address of a STORED row is read only, and that is the point rather than an
 * omission: see this file's own opening note. A new row's address lives here and
 * reaches the draft only when it is one the server will take.
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
  /** Already in the draft, and therefore already a subscription with a memory. */
  stored: boolean;
  open: boolean;
  priorities: { id: string; label: string }[];
  onToggle: () => void;
  /** This row's health, when the server has anything to say about it yet. */
  status?: FeedStatus;
  onCommitUrl: (typed: string) => Verdict;
  onChange: (next: FeedSubscription) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  const [text, setText] = useState(row.url);
  const [verdict, setVerdict] = useState<Verdict>('blank');

  // The filter is typed here and committed on the way out, for the same reason
  // the address is: a pattern that will not compile is REFUSED by the server and
  // takes the whole settings document with it, so "(" on its way to "(a|b)" must
  // never be what a 600 ms autosave finds in the draft.
  const [filter, setFilter] = useState(row.titleFilter ?? '');
  const [filterBad, setFilterBad] = useState(false);

  // The save answer replaces the draft, so a filter the server trimmed comes back
  // spelled differently from what was typed. Follow it rather than holding the
  // old text on screen: what came back is what this subscription now does.
  useEffect(() => {
    setFilter(row.titleFilter ?? '');
    setFilterBad(false);
  }, [row.titleFilter]);

  const commitUrl = () => setVerdict(onCommitUrl(text));

  const commitFilter = () => {
    const next = filter.trim();
    // An identical patch still marks the whole draft dirty, and a Save bar that
    // lights up because somebody looked at a field is a Save bar nobody trusts.
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

  // What the poller will actually do, which for a stored 0 is the server's own
  // quarter of an hour. Shown quieter than a number somebody typed, because it is
  // the default answering and not a value this row holds - the field itself still
  // reads 0, and this column would be lying if it made the two look alike.
  const derived = row.intervalMinutes === 0;
  const effective = derived ? DEFAULT_INTERVAL_MINUTES : row.intervalMinutes;

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="group grid grid-cols-[1fr_auto] items-center gap-3 py-2.5">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          className="flex min-w-0 items-center gap-3 text-left"
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          {/* dir=ltr and truncated with the whole address as the tooltip: an
              address is never read right to left whatever the interface language
              is, and a feed URL is routinely longer than the row. */}
          <span dir="ltr" title={row.url} className="min-w-0 flex-1 truncate text-sm text-carbon-text">
            {row.url || <span className="text-carbon-textMuted">{t('settings.feeds.url')}</span>}
          </span>
          {/* What this row overrides, named by the field it comes from. Nothing
              here reports on the feed itself: there is no route that could. */}
          {row.titleFilter && <Marker icon={<IconFilter width={14} height={14} />} title={t('settings.feeds.filter')} />}
          {row.dir && <Marker icon={<IconFolder width={14} height={14} />} title={t('settings.feeds.dir')} />}
          {row.priority !== undefined && (
            <Marker icon={<IconPriority width={14} height={14} />} title={t('settings.feeds.priority')} />
          )}
          <span
            className={`glim-num hidden w-12 shrink-0 text-end text-xs sm:block ${
              derived ? 'text-carbon-textMuted' : 'text-carbon-textSub'
            }`}
            title={t('settings.feeds.interval')}
          >
            {effective}
          </span>
        </button>
        {/* The one row action, on hover and on keyboard focus, so a long list
            reads as content rather than as a wall of buttons. */}
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            kind="danger"
            icon={<IconTrash width={14} height={14} />}
            hue={index}
            title={t('settings.feeds.remove')}
            aria-label={t('settings.feeds.remove')}
            // Keeping focus in the address box means no blur, and therefore no
            // commit, in front of this click. Without it a new row whose address
            // was just typed would be written to the draft on the way out and
            // this press would then remove a row that no longer exists.
            onMouseDown={(e) => e.preventDefault()}
            onClick={onRemove}
          />
        </div>
      </div>

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          <Field label={t('settings.feeds.url')} hint={t('settings.feeds.urlHint')}>
            {stored ? (
              // Read only rather than absent, so the address can still be read
              // and copied out of the row it belongs to. It is not editable
              // because editing it does not correct this subscription, it
              // replaces it with one that has forgotten everything the first one
              // added: remove and add is the same operation said out loud.
              <TextInput
                dir="ltr"
                readOnly
                spellCheck={false}
                value={row.url}
                // Dimmed with opacity rather than a quieter ink colour: two
                // text-colour utilities on one element are resolved by their
                // order in the compiled stylesheet and not by the order they
                // were written in, so the quieter one is not reliably the one
                // that wins.
                className="cursor-default opacity-70"
              />
            ) : (
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={text}
                placeholder="https://example.org/feed.xml"
                // aria-invalid and no commit, rather than a corrected value: the
                // typing is kept, and the collapsed row above still shows what is
                // really stored, which is the honest difference between the two.
                // The halo is the same shape the focus ring uses, and loses to it
                // while the box is focused, which is where a correction is being
                // made anyway.
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
          {/* The state of this row, not an explanation of the field - the
              explanation is behind the (i) on the label. */}
          {verdict === 'duplicate' && <p className="text-xs text-statusWarn">{t('settings.feeds.duplicate')}</p>}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {/* 0 is a value here and not a blank: it means this row has no
                opinion about the pace and takes the server's quarter of an hour.
                min is therefore 0 and not 1, and the number is stored as typed;
                anything else non-zero is cut into 1..10080 exactly as
                feed.Sanitize would cut it, so the field does not change under
                the user once the save comes back. */}
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
                // Marked and not committed, never corrected: a filter that is
                // thrown away for the user is a subscription quietly collecting
                // the whole feed. The one thing that does not survive collapsing
                // the row is a pattern that will not compile, which is precisely
                // the one thing that must never reach the draft.
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

          {/* PathInput and not a plain box: this folder may be a pathvars
              template, and browsing has to replace the fixed part in front of the
              first placeholder and nothing else. Its own title, so the chooser's
              heading does not read as the main download folder. Nothing about
              this path is checked while saving - it is checked when an entry
              actually arrives, and a folder that fails there takes the priority
              below down with it, because the two are written in one call. */}
          <Field label={t('settings.feeds.dir')} hint={t('settings.feeds.dirHint')}>
            <PathInput
              value={row.dir ?? ''}
              title={t('settings.feeds.dir')}
              onValue={(dir) => onChange(dir.trim() === '' ? without(row, 'dir') : { ...row, dir })}
            />
          </Field>

          {/* A FieldGroup and not a Field: a Field is a <label>, and a label
              hands a click on its caption to the first control inside it, which
              here would silently set the highest priority. */}
          {priorities.length > 0 && (
            <FieldGroup label={t('settings.feeds.priority')} hint={t('settings.feeds.priorityHint')}>
              <Tabs
                size="sm"
                label={t('settings.feeds.priority')}
                // Absent and 0 are different values, so the neutral item is
                // matched on undefined and never on the number: a subscription
                // that asks for 0 is asking for a real priority.
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
 * What the server knows about this subscription right now.
 *
 * Only ever drawn for a STORED row: a pending one has no address the poller has
 * ever seen, so every line here would read as a fault on a subscription that
 * does not exist yet.
 *
 * The absent-status case is the one that matters and it is deliberately quiet.
 * A subscription with no lastPolledAt has not been looked at SINCE THE SERVER
 * STARTED, which is a different statement from "never", because this table is
 * in memory and the subscription's own record of what it has added is not. So
 * seeded and remembered are withheld in that state rather than drawn as false
 * and zero: printing "has not seeded, remembers nothing" a second after a
 * restart would be a false alarm on a feed somebody has followed for a year.
 */
function FeedHealth({ status }: { status?: FeedStatus }) {
  const { t } = useT();
  const known = status?.lastPolledAt !== undefined;

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
        {/* The reason a row is not being polled and the reason its last look
            failed are two different sentences, and the server tells them apart
            by `polling`. One string for both would report a refused row as a
            transient network problem it will get over. */}
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

/** An RFC3339 stamp in the reader's own locale, or the raw string when the
 *  server sends something this browser will not parse. Never a relative
 *  "3 minutes ago": this value is refreshed once, on mount, so a relative time
 *  would go on ageing on screen while the number behind it stood still. */
function fmtWhen(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

/**
 * Fetch this feed once and show what is in it.
 *
 * It exists for the title filter. A filter is a regular expression matched
 * against titles the person writing it has never seen, and every other way of
 * finding out what a feed carries costs a saved subscription and a wait.
 *
 * THREE THINGS IT MUST NOT DO, and each is a promise the route already keeps:
 * it stages nothing, it remembers nothing, and it works on an address that has
 * not been saved. The last one is most of the value, so it is deliberately not
 * gated on the row being stored.
 *
 * Two shapes of failure, drawn differently on purpose. A row that is itself
 * wrong (an address that is not http or https, a pattern Go will not compile)
 * throws with the server's own sentence, which names the field. A feed that
 * simply could not be read comes back as a normal result with `error` set. The
 * first is something to fix here; the second is something about somebody else's
 * server, and showing them the same way would send people looking in the wrong
 * place.
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
                            className={`shrink-0 text-[10px] uppercase tracking-wider ${
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
            {/* Said at the result and not only in the bubble above: somebody
                who has just pressed a button on a stranger's address wants to
                read here, not hover there, that nothing was taken. */}
            <p className="text-carbon-textMuted">{t('settings.feeds.testSafe')}</p>
          </div>
        )}
      </div>
    </FieldGroup>
  );
}

/** One mark in the collapsed row, named by the field it stands for. Present only
 *  when that field is set, so a row of marks says what this subscription
 *  overrides and never how the feed itself is doing. */
function Marker({ icon, title }: { icon: ReactNode; title: string }) {
  return (
    <span role="img" aria-label={title} title={title} className="hidden shrink-0 text-carbon-textMuted sm:block">
      {icon}
    </span>
  );
}
