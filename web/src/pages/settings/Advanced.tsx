import { Fragment, useEffect, useMemo, useState } from 'react';
import { fetchOptions, type Settings } from '../../lib/api';
import { reasonKey } from '../../components/columns';
import { Tabs } from '../../components/Tabs';
import {
  Card,
  FieldGroup,
  IconBadge,
  InfoBubble,
  NumberInput,
  SectionTitle,
  TextArea,
  TextInput,
  ToggleRow,
} from '../../components/ui';
import { IconRetry, IconSearch } from '../../lib/icons';
import type { TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { useResource } from '../../lib/useResource';
import { useDraft } from './context';
import { NeutralSwitch } from './controls';
import { fetchSettingsSchema, type SettingsSchema } from './features';
import { getPath, rowsFor, same, setPath, type Row, type ValueKind } from './paths';
import { useTx } from './tx';

/**
 * Every setting this instance has, by name.
 *
 * The reason it exists at all: JDownloader's Advanced Settings is how its users
 * actually change things, and half the questions in its forum are answered with
 * a key name. Ours had no equivalent — a setting with no control on a page was a
 * setting nobody could reach. This table is generated from the settings document
 * the server sends, so a field a later wave adds appears here the day it lands,
 * with no edit to this file.
 *
 * It edits the same draft the other pages do, so the one save bar covers it and
 * a change made here and a change made on Downloads go out in one request. That
 * also means every value written here goes through the server's own sanitize and
 * validate on save: a raw edit cannot bypass a clamp.
 *
 * Above the table sit the settings that have no page of their own but are too
 * consequential to leave as a key path: the mirror set, what happens to a link
 * already known to be dead, how much the "already on the disk" pass may believe,
 * and the attempt count per kind of failure. A key path is reachable, which is
 * not the same as configured - the raw row for `mirrorPolicy` is a text box with
 * no menu of legal values, no label and no explanation, and a wrong word in it
 * is folded onto the default without a word said. The keys keep their raw rows
 * as well; see the comment on the first card for why that duplication is
 * deliberate.
 */

// Not settings, and so not editable: the rule-compile problems ride along in the
// same response as output. A reset button on them would be a control with
// nothing behind it.
const NOT_SETTINGS = ['problems'] as const;

// What the reconnect package puts in place of a stored password. Matching it is
// how the row knows to say "leave this alone to keep the stored one" instead of
// showing eight asterisks with no explanation.
const REDACTED = '********';

/**
 * The failure words the retry table is keyed on, taken from the download list
 * rather than written out a second time.
 *
 * columns.tsx's reasonKey is already the one place those nine words live: the
 * task rows and the failure chips both read it. A copy here is how a spinner
 * ends up labelled "Hoster limit" over rows the list calls something else, and
 * how a reason a later build adds gets a row in one place and not in the other.
 * Its insertion order is the order the chips are drawn in, so the table and the
 * list read the same way down the page.
 *
 * Nothing on the server serves this vocabulary yet. When something does, this
 * constant is the single line that changes.
 */
const RETRY_REASONS = Object.entries(reasonKey);

// settings.maxRetryTries. sanitizeRetryRule cuts anything above this down to 20
// and a negative up to 0 on save, silently, so the spinner has to stop at the
// same number: a control that offers 50 and stores 20 is a control that lies
// about what saving it did.
const MAX_TRIES = 20;

/** The three menu prefixes this page looks value names up under. */
type ChoicePrefix = 'settings.advanced.mirror.' | 'settings.advanced.offline.' | 'settings.advanced.reclaim.';

/**
 * choices names the values a server-sent menu offers, by lookup and never by a
 * switch over ids this file carries.
 *
 * The membership test is against `en` and not against the loaded dictionary, for
 * the reason tx.ts's own label() gives: every locale is typed as Dict and so
 * carries exactly the same keys, and English is the one that loads
 * synchronously. The fallback is the part that earns the helper, though: t()
 * with a key the catalogue does not have returns undefined, so a policy the
 * server learns before this build has a word for it would paint a BLANK tab,
 * which reads as a broken page. Under its raw id it is at least a value somebody
 * can recognise, search for and report.
 */
function choices(tx: (k: TranslationKey) => string, prefix: ChoicePrefix, ids: string[]) {
  return ids.map((id) => {
    const key = (prefix + id) as TranslationKey;
    return { id, label: key in en ? tx(key) : id };
  });
}

export function Advanced() {
  const { tx } = useTx();
  const { cfg, patch, replace } = useDraft();
  const doc = cfg as unknown as Record<string, unknown>;

  // Every menu on the curated cards comes from GET /api/options, never from a
  // list in this file: a strip built out of this build's own guesses goes on
  // offering a word the server folds away on save, which looks exactly like the
  // setting refusing to stick. A list an older server has not learnt to send
  // arrives undefined rather than empty, hence the `?? []` on each.
  const { data: options } = useResource(fetchOptions);
  const mirrorPolicies = options?.mirrorPolicies ?? [];
  const confirmPolicies = options?.confirmPolicies ?? [];
  const reclaimModes = options?.reclaimTrustModes ?? [];

  const [schema, setSchema] = useState<SettingsSchema | null>(null);
  const [schemaFailed, setSchemaFailed] = useState(false);
  const [query, setQuery] = useState('');
  const [searchOpen, setSearchOpen] = useState(false);
  // Debounced, because the filter runs over the whole flattened document and
  // re-renders every visible editor: typing "down" would otherwise rebuild the
  // table four times, and the input would stutter on exactly the keys somebody
  // is trying to search for.
  const [needle, setNeedle] = useState('');
  const [onlyModified, setOnlyModified] = useState(false);

  useEffect(() => {
    const id = setTimeout(() => setNeedle(query.trim().toLowerCase()), 150);
    return () => clearTimeout(id);
  }, [query]);

  useEffect(() => {
    let live = true;
    fetchSettingsSchema()
      .then((d) => live && setSchema(d))
      .catch(() => live && setSchemaFailed(true));
    return () => {
      live = false;
    };
  }, []);

  const rows = useMemo(() => rowsFor(doc, schema?.kinds ?? {}, NOT_SETTINGS), [doc, schema]);

  const shown = useMemo(() => {
    return rows.filter((r) => {
      if (onlyModified && (!schema || same(r.value, getPath(schema.values, r.path)))) return false;
      if (!needle) return true;
      // The value is searched as well as the key, because half the time what
      // somebody remembers is the folder they typed, not what the field is called.
      //
      // `?? null` is load-bearing: JSON.stringify(undefined) returns undefined,
      // not "undefined", and every key that omitempty dropped from the document
      // has exactly that value — so without it the first keystroke in this box
      // takes the whole page down.
      const text = JSON.stringify(r.value ?? null) ?? 'null';
      return r.path.toLowerCase().includes(needle) || text.toLowerCase().includes(needle);
    });
  }, [rows, needle, onlyModified, schema]);

  function write(path: string, value: unknown) {
    replace(setPath(doc, path, value) as unknown as Settings);
  }

  // Read through `?.` and `?? {}` at every level: retry is written by the
  // server, and Go encodes an empty map as JSON null rather than as {} - the
  // same pairing hostRules already has. A document from a server that predates
  // the key has no retry object at all.
  const rules = cfg.retry?.byReason ?? {};

  /**
   * setTries writes one failure's attempt count into the per-reason table.
   *
   * Two things it deliberately does NOT do. It does not store a 0: the Go field
   * carries omitempty, so absent and 0 are the same statement here ("no opinion,
   * take the count from the Downloads page"), and a rule dropped back to 0 with
   * nothing else in it is removed from the map instead, so the saved document
   * does not grow a row per reason somebody once typed into and cleared again.
   * And it does not rebuild the policy: retry.delay and retry.max are the
   * instance-wide backoff, they belong to another control, and rebuilding the
   * object here would quietly clear them.
   */
  function setTries(reason: string, n: number) {
    const rest = { ...rules[reason] };
    delete rest.tries;
    const next = { ...rules };
    if (n > 0) next[reason] = { ...rest, tries: n };
    else if (Object.keys(rest).length > 0) next[reason] = rest;
    else delete next[reason];
    patch({ retry: { ...(cfg.retry ?? { delay: 0, max: 0, byReason: {} }), byReason: next } });
  }

  return (
    <div className="flex flex-col gap-10">
      {/* The named controls first, the generated key table last: that is the
          order the page is read in, an explained setting before the raw path for
          everything that has none.

          The five keys these cards own keep their rows in that table as well,
          and deliberately so. NOT_SETTINGS above is for things that are not
          settings at all, and the table's contract is every key by name, so
          dropping them from it would make the page lie about what it lists.
          Both halves edit the same draft through useDraft, so the switch up here
          and the row down there can never disagree, and one save covers both. */}

      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle>{tx('settings.advanced.mirrorsTitle')}</SectionTitle>

        {/* FieldGroup and not Field: a Field is a `<label>`, and a label around a
            tab strip hands a click on its caption to the first tab, so clicking
            the words "When two links count as the same file" would set the
            policy to whatever happens to be leftmost. Archives.tsx carries the
            same note for the same reason.

            Drawn only once the server has answered. dedupe.ParsePolicy folds
            anything it does not recognise, the empty string included, onto
            filename-and-size and never refuses the save - so a strip this build
            guessed at would not fail loudly, it would quietly store the default
            over whatever was chosen. */}
        {mirrorPolicies.length > 0 && (
          <FieldGroup
            layout="row"
            label={tx('settings.advanced.mirrorPolicy')}
            hint={tx('settings.advanced.mirrorPolicyHint')}
          >
            <Tabs
              variant="well"
              label={tx('settings.advanced.mirrorPolicy')}
              active={cfg.mirrorPolicy ?? ''}
              onSelect={(mirrorPolicy) => patch({ mirrorPolicy })}
              items={choices(tx, 'settings.advanced.mirror.', mirrorPolicies)}
            />
          </FieldGroup>
        )}

        {/* Not dimmed when the policy above is off, even though nothing is ever
            a mirror then. The policy decides what is DETECTED and these two
            decide what becomes of a detection; a control that greys itself out
            teaches nobody what the mode can do, so the bubble says it in words
            instead. */}
        <ToggleRow
          hue={0}
          checked={cfg.keepMirrors ?? false}
          onChange={(keepMirrors) => patch({ keepMirrors })}
          label={tx('settings.advanced.keepMirrors')}
          hint={tx('settings.advanced.keepMirrorsHint')}
        />

        {/* Dimmed and never hidden while the switch above is off: with no parked
            copy there is nothing for this one to release. ToggleRow's disabled
            state keeps its own (i) hoverable on purpose (see ui.tsx), and that
            bubble is the only place the sentence explaining the dimming lives -
            hiding the row would take the explanation away with it. */}
        <ToggleRow
          hue={1}
          checked={cfg.mirrorFailover ?? false}
          onChange={(mirrorFailover) => patch({ mirrorFailover })}
          label={tx('settings.advanced.mirrorFailover')}
          hint={tx('settings.advanced.mirrorFailoverHint')}
          disabled={!cfg.keepMirrors}
        />
      </Card>

      {/* Gated on the menu rather than rendered empty: this card holds exactly
          one control, and a title badge floating over nothing reads as a
          rendering fault. "use-global" never reaches the strip - the server
          withholds it, because a global default cannot defer to itself. */}
      {confirmPolicies.length > 0 && (
        <Card hue={1} className="flex flex-col gap-5">
          <SectionTitle>{tx('settings.advanced.offlineTitle')}</SectionTitle>
          <FieldGroup
            layout="row"
            label={tx('settings.advanced.onOffline')}
            hint={tx('settings.advanced.onOfflineHint')}
          >
            <Tabs
              variant="well"
              label={tx('settings.advanced.onOffline')}
              active={cfg.onOffline ?? ''}
              onSelect={(onOffline) => patch({ onOffline })}
              items={choices(tx, 'settings.advanced.offline.', confirmPolicies)}
            />
          </FieldGroup>
        </Card>
      )}

      {/* The tiers are served strictest first and the strip keeps that order, so
          the row reads as a scale rather than as three unrelated words. Nothing
          is drawn at all where the list is missing, which is also what a client
          talking to a server that does not serve it yet gets: reclaim.ParseTrust
          folds an unrecognised value onto "record", which is deliberately NOT
          the strictest tier, so a guessed strip here would quietly loosen what
          the pass is allowed to believe about a file it never watched arrive. */}
      {reclaimModes.length > 0 && (
        <Card hue={2} className="flex flex-col gap-5">
          <SectionTitle>{tx('settings.advanced.reclaimTitle')}</SectionTitle>
          <FieldGroup
            layout="row"
            label={tx('settings.advanced.reclaimTrust')}
            hint={tx('settings.advanced.reclaimTrustHint')}
          >
            <Tabs
              variant="well"
              label={tx('settings.advanced.reclaimTrust')}
              active={cfg.reclaimTrust ?? ''}
              onSelect={(reclaimTrust) => patch({ reclaimTrust })}
              items={choices(tx, 'settings.advanced.reclaim.', reclaimModes)}
            />
          </FieldGroup>
        </Card>
      )}

      <Card hue={3} className="flex flex-col gap-5">
        <SectionTitle>{tx('settings.advanced.retryTitle')}</SectionTitle>
        {/* One bubble on the whole grid and not one per row: the sentence is the
            same nine times over, and nine identical (i) glyphs down a column are
            noise rather than help. It carries the two things a spinner cannot
            say for itself - that 0 hands the question to the general count on
            the Downloads page rather than meaning "never", and that a host rule
            beats this, which beats that count, resolved field by field.

            There is no second attempt-count spinner here on purpose: the number
            these rows fall back to is settings.maxRetries, which already has its
            own control on the Downloads page, and a second copy of it is how the
            two drift apart. */}
        <FieldGroup label={tx('settings.advanced.retryTries')} hint={tx('settings.advanced.retryTriesHint')}>
          {/* Scrolls inside its own box. Letting the page scroll sideways
              instead would take the settings rail and the save bar off screen
              with it, which is the same reason the key table below does it.
              A grid rather than a stack of rows so that delay, ceiling and
              "never" can arrive later as further COLUMNS, instead of as a
              second table repeating the same nine words. */}
          <div className="overflow-x-auto">
            <div className="grid min-w-[18rem] grid-cols-[1fr_auto] items-center gap-x-4 gap-y-2">
              {RETRY_REASONS.map(([reason, labelKey]) => (
                <Fragment key={reason}>
                  <span className="text-sm text-carbon-text">{tx(labelKey)}</span>
                  {/* The spinner takes the failure's own word as its accessible
                      name: the caption beside it is a plain span rather than a
                      label, because wrapping nine rows in nine labels is nine
                      more captions than the one on the group above. */}
                  {/* max is the server's own ceiling and not a number picked
                      here: sanitizeRetryRule cuts a 50 to 20 without saying so,
                      and a spinner that let one be typed would be reporting a
                      value the instance never stored. */}
                  <div className="w-28 sm:w-32">
                    <NumberInput
                      aria-label={tx(labelKey)}
                      value={rules[reason]?.tries ?? 0}
                      min={0}
                      max={MAX_TRIES}
                      onValue={(n) => setTries(reason, n)}
                    />
                  </div>
                </Fragment>
              ))}
            </div>
          </div>
        </FieldGroup>
      </Card>

      {/* The filter row and the table it filters stay one block at the tighter
          gap: the search badge and the "only what differs" switch describe what
          is directly under them, and spacing them apart like two cards would
          leave them reading as page-level furniture. */}
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-3">
          {/* Disabled until the factory values arrive: "only what differs from
              the default" with no defaults to compare against would answer "all
              of it", which is the opposite of what it says. */}
          <NeutralSwitch
            on={onlyModified}
            onChange={setOnlyModified}
            disabled={!schema}
            name={tx('settings.advanced.onlyModified')}
          />
          <span className="text-xs text-carbon-textSub">{tx('settings.advanced.onlyModified')}</span>
          {schemaFailed && (
            <span className="text-xs text-statusWarn">{tx('settings.advanced.defaultsUnavailable')}</span>
          )}
          <span className="flex-1" />
          {/* A square badge that opens into the field, not an always-visible
              search box (jdp, 2026-08-23: "Die suche soll rechts oben als
              quadratischer badge mit glyph angezeigt werden und bei klick
              soll das suchfeld ausklappen") - stays open while there is text
              in it, so an active filter is never hidden without the user
              seeing it. */}
          {searchOpen || query ? (
            <div className="min-w-0 sm:max-w-xs">
              <TextInput
                autoFocus={searchOpen}
                type="search"
                spellCheck={false}
                value={query}
                placeholder={tx('settings.advanced.search')}
                aria-label={tx('settings.advanced.search')}
                onChange={(e) => setQuery(e.target.value)}
                onBlur={() => {
                  if (!query) setSearchOpen(false);
                }}
              />
            </div>
          ) : (
            <IconBadge
              icon={<IconSearch width={16} height={16} />}
              title={tx('settings.advanced.search')}
              aria-label={tx('settings.advanced.search')}
              onClick={() => setSearchOpen(true)}
            />
          )}
        </div>

        {/* hue 4, after the four curated cards above it. The position is the
            card's place in the palette and not a name, so a card inserted in
            front of this one moves it along rather than leaving two cards
            sharing a colour in rainbow mode. */}
        <Card hue={4} padding="none">
          <div className="p-5 pb-0">
            <SectionTitle>{tx('settings.advanced.allSettings')}</SectionTitle>
          </div>
          {/* The table scrolls inside its own box. A key path plus a value editor
              is wider than a phone, and letting the page scroll sideways instead
              would take the rail and the save bar off screen with it. */}
          <div className="overflow-x-auto">
            <div className="min-w-[34rem]">
              {shown.length === 0 ? (
                <p className="p-6 text-center text-sm text-carbon-textMuted">{tx('settings.advanced.noMatch')}</p>
              ) : (
                shown.map((r, i) => (
                  <KeyRow
                    key={r.path}
                    row={r}
                    hue={i}
                    fallback={schema ? getPath(schema.values, r.path) : undefined}
                    canReset={schema !== null}
                    onWrite={write}
                  />
                ))
              )}
            </div>
          </div>
        </Card>
      </div>
    </div>
  );
}

function KeyRow({
  row,
  hue,
  fallback,
  canReset,
  onWrite,
}: {
  row: Row;
  hue: number;
  fallback: unknown;
  canReset: boolean;
  onWrite: (path: string, value: unknown) => void;
}) {
  const { tx } = useTx();
  const modified = canReset && !same(row.value, fallback);
  const secret = row.value === REDACTED;

  return (
    <div className="flex flex-col gap-2 px-4 py-3 odd:bg-carbon-surface2/30 sm:flex-row sm:items-center sm:gap-4">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex items-center">
          {/* A key path is an identifier: it stays left-to-right even in an RTL
              locale, where the dots would otherwise be shuffled to the far end
              and the path would name a key that does not exist. */}
          <code dir="ltr" className="truncate text-xs text-carbon-text">
            {row.path}
          </code>
          {secret && <InfoBubble tip={tx('settings.advanced.secret')} />}
          {row.kind === 'list' && <InfoBubble tip={tx('settings.advanced.listHint')} />}
        </span>
        <span className="flex items-center gap-2 text-[11px] text-carbon-textMuted">
          {tx(`settings.advanced.type.${row.kind}` as `settings.advanced.type.${ValueKind}`)}
          {modified && <span className="text-carbon-textSub">· {tx('settings.advanced.modified')}</span>}
        </span>
      </div>

      <div className="w-full sm:w-72">
        <ValueEditor row={row} hue={hue} onWrite={onWrite} />
      </div>

      {/* Reset appears only where there is something to undo, so the column is
          not a wall of buttons on a page that is already dense. A square
          glyph badge (jdp, 2026-08-23: "Alle zurücksetzten sollen ein
          quadratischer badge mit glyph werden"), matching every other
          row-scoped action in the app (AccountsTable's own gear badge,
          Rules.tsx's row actions) instead of a text button. */}
      <div className="w-8 shrink-0">
        {modified && (
          <IconBadge
            icon={<IconRetry width={16} height={16} />}
            hue={hue}
            title={tx('settings.advanced.resetTitle')}
            aria-label={tx('settings.advanced.resetTitle')}
            onClick={() => onWrite(row.path, fallback ?? emptyFor(row.kind))}
          />
        )}
      </div>
    </div>
  );
}

function ValueEditor({
  row,
  hue,
  onWrite,
}: {
  row: Row;
  hue: number;
  onWrite: (path: string, value: unknown) => void;
}) {
  const { tx } = useTx();
  // A list is edited as text and only written back once it parses. Writing a
  // half-typed array into the draft would let the save button send `[{"na` as a
  // rule set, and the server would answer with a JSON error naming nothing the
  // user recognises.
  const [text, setText] = useState(() => JSON.stringify(row.value ?? [], null, 1));
  const [badJSON, setBadJSON] = useState(false);

  switch (row.kind) {
    case 'boolean':
      return (
        <NeutralSwitch
          on={row.value === true}
          name={row.path}
          onChange={(v) => onWrite(row.path, v)}
          hue={hue}
        />
      );
    case 'number':
      return (
        <NumberInput
          value={typeof row.value === 'number' ? row.value : 0}
          onValue={(v) => onWrite(row.path, v)}
        />
      );
    case 'list':
      return (
        <div className="flex flex-col gap-1">
          <TextArea
            dir="ltr"
            rows={3}
            spellCheck={false}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              try {
                const parsed = JSON.parse(e.target.value);
                setBadJSON(false);
                onWrite(row.path, parsed);
              } catch {
                setBadJSON(true);
              }
            }}
          />
          {badJSON && <span className="text-[11px] text-statusFail">{tx('settings.advanced.badJson')}</span>}
        </div>
      );
    default:
      return (
        <TextInput
          dir="ltr"
          spellCheck={false}
          value={row.value === null || row.value === undefined ? '' : String(row.value)}
          onChange={(e) => onWrite(row.path, e.target.value)}
        />
      );
  }
}

/**
 * emptyFor is the reset value for a key the defaults document does not carry.
 *
 * Go's `omitempty` drops a zero-valued field on the way out, so "no default" and
 * "the default is empty" arrive identically. Resetting to `undefined` would drop
 * the key out of the document that PUT /api/settings replaces wholesale — so the
 * reset writes the zero value of the key's declared type instead, which is why
 * this takes the kind and not the current value: for an empty list those two
 * disagree, and the wrong one sends a string where an array is expected.
 */
function emptyFor(kind: ValueKind): unknown {
  switch (kind) {
    case 'boolean':
      return false;
    case 'number':
      return 0;
    case 'list':
      return [];
    default:
      return '';
  }
}
