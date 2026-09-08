import { useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  Field,
  IconBadge,
  NumberInput,
  SectionTitle,
  TextInput,
  ToggleRow,
} from '../../../components/ui';
import { IconBolt, IconDownloads, IconPlus, IconRetry, IconTrash, IconWarning } from '../../../lib/icons';
import { useT } from '../../../lib/i18n';
import type { HostRule, RetryRule } from '../../../lib/api';
import { useDraft } from '../context';

/**
 * The per-host exceptions to the counts on the card above: how much one hoster
 * may have open, how many sockets one of its downloads gets, and how patiently
 * a failure from it is retried.
 *
 * Three things about this card are decisions rather than layout.
 *
 * IT IS A MAP, NOT A LIST. settings.hostRules is keyed by the host pattern, so
 * a row has no id of its own and the name IS the identity. Everything awkward
 * below follows from that: a half-typed name may not be written (typing
 * "rapidgator.net" into the draft key by key would leave r, ra, rap… behind as
 * real rows), a row with no name at all may not be written (sanitizeHostRules,
 * internal/settings/settings_hostrules.go, drops a blank pattern on save
 * without a word, so the row would disappear and nothing would say why), and a
 * rename is delete-then-set with the value carried across, because a map has no
 * rename.
 *
 * ZERO IS "NO OPINION" ON EVERY NUMBER HERE, never "off" and never
 * "unlimited". Each field falls through to the level below it, field by field,
 * so a row that sets only the wait leaves the attempt count to whatever the
 * levels below say. That is why every label carries its own "(0 = …)" and why
 * the summary column leaves a zero blank instead of printing it.
 *
 * THE CEILINGS ARE THE SERVER'S, not numbers picked here: 64 simultaneous
 * downloads (maxConcurrentCeiling), 16 connections (rules.MaxChunks), 20
 * attempts (maxRetryTries) and one day of waiting (maxRetryWait). The server
 * silently cuts anything above them while saving, so a spinner that went higher
 * would be a control that lies about what saving it did.
 */

/** Simultaneous downloads from one host - maxConcurrentCeiling. */
const MAX_PER_HOST = 64;
/** Connections one download opens - rules.MaxChunks; the engine opens no more. */
const MAX_CHUNKS = 16;
/** Attempts - maxRetryTries, the same ceiling the global count has. */
const MAX_TRIES = 20;
/** One day, in seconds - maxRetryWait, applied by clampSeconds to both waits. */
const MAX_WAIT = 86400;

/**
 * normalizeHostPattern, in TypeScript: case, the stray whitespace of a pasted
 * line, a leading "*." and the dots of a fully qualified name all fold into one
 * spelling.
 *
 * It has to agree with the Go side exactly, because two raw keys that normalise
 * the same both survive the save and only ONE of them is ever consulted
 * (HostRuleFor picks the longest match and settles ties by raw key order). The
 * other sits on the page looking configured and answering nothing, which is the
 * kind of row nobody can explain a month later - so the editor refuses the
 * second one instead of storing it.
 */
function normalizeHost(raw: string): string {
  const trimmed = raw.trim().toLowerCase();
  const bare = trimmed.startsWith('*.') ? trimmed.slice(2) : trimmed;
  return bare.replace(/^\.+/, '').replace(/\.+$/, '');
}

/**
 * Whole, non-negative, and never above the server's own ceiling.
 *
 * Cut here as well as on the server on purpose: the save cuts it silently, and
 * a box that keeps showing 900 after a save that stored 64 is a page telling
 * the user something that is not true about their own install.
 */
function clampInt(v: number, max: number): number {
  if (!Number.isFinite(v)) return 0;
  return Math.min(Math.max(0, Math.round(v)), max);
}

/** What committing a typed host did, so the row can say why nothing happened. */
type Verdict = 'ok' | 'blank' | 'duplicate';

/**
 * A row that has been added but has no host yet, and therefore no map key to
 * live under. It stays in component state until it earns one; writing it to the
 * draft early is the exact mistake that makes a row vanish on save.
 */
interface PendingRow {
  id: string;
  host: string;
  rule: HostRule;
}

let pendingCounter = 0;
const freshId = () => `p${(pendingCounter++).toString(36)}`;

/**
 * Row identity for React and for "which row is open".
 *
 * A stored row is identified by its map key and a new one by its client-side
 * id, and the two namespaces are kept apart by a prefix rather than trusted to
 * differ: a host pattern is whatever somebody types, so an id that merely looks
 * unlikely to collide is an id that collides once.
 */
const storedId = (key: string) => `k:${key}`;

export function HostRulesCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // null and {} both arrive on the wire and mean the same thing: the Go field
  // has no omitempty, so an install whose settings.json predates the key sends
  // null while Defaults() writes {}.
  const rules = cfg.hostRules ?? {};
  // Sorted by raw key, which is also how the server settles two patterns of
  // equal length, so the order on the page and the order it resolves ties in
  // are the same order. Insertion order would put a renamed row last and make
  // it look as if it had moved.
  const keys = Object.keys(rules).sort();

  const [openRow, setOpenRow] = useState('');
  const [pending, setPending] = useState<PendingRow[]>([]);

  const write = (next: Record<string, HostRule>) => patch({ hostRules: next });

  /**
   * Put `rule` under the typed host, taking it out from under `fromKey` if it
   * had one. Refuses rather than writing when the name would be thrown away or
   * would shadow a row that already exists.
   */
  const commit = (fromKey: string, typed: string, rule: HostRule): Verdict => {
    const next = normalizeHost(typed);
    if (next === '') return 'blank';
    // Leaving the field without having changed anything must not write: an
    // identical patch still marks the whole draft dirty, and a Save bar that
    // lights up because somebody looked at a row is a Save bar nobody trusts.
    if (next === fromKey) return 'ok';
    if (next !== normalizeHost(fromKey) && keys.some((k) => k !== fromKey && normalizeHost(k) === next)) {
      return 'duplicate';
    }
    const map = { ...rules };
    // delete-then-set, because a map has no rename - and the value goes across
    // with it, so renaming a host does not quietly reset its numbers.
    delete map[fromKey];
    map[next] = rule;
    write(map);
    return 'ok';
  };

  const add = () => {
    // A row with no host yet cannot be stored, so a second press of Add would
    // only stack a second one that also cannot be stored - two nameless rows
    // that look identical and both disappear on save. The one already waiting
    // is opened instead.
    const waiting = pending.find((r) => normalizeHost(r.host) === '');
    if (waiting) {
      setOpenRow(waiting.id);
      return;
    }
    const row: PendingRow = { id: freshId(), host: '', rule: {} };
    setPending((p) => [...p, row]);
    setOpenRow(row.id);
  };

  const rowCount = keys.length + pending.length;

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle
        hint={t('settings.hostRules.titleHint')}
        right={
          <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
            {t('settings.hostRules.add')}
          </Button>
        }
      >
        {t('settings.hostRules.title')}
      </SectionTitle>

      {rowCount === 0 ? (
        // Inside the card rather than instead of it: the Add button above is the
        // only way out of this state, and swapping the card for an EmptyState
        // would take it off the page.
        <p className="py-6 text-center text-sm text-carbon-textSub">
          {t('settings.hostRules.empty')}
          <span className="mt-1 block text-[11px] text-carbon-textMuted">{t('settings.hostRules.emptyHint')}</span>
        </p>
      ) : (
        <ul className="flex flex-col">
          {keys.map((key, i) => (
            <HostRuleRow
              key={storedId(key)}
              host={key}
              rule={rules[key] ?? {}}
              index={i}
              last={i === rowCount - 1}
              open={openRow === storedId(key)}
              onToggle={() => setOpenRow(openRow === storedId(key) ? '' : storedId(key))}
              onCommit={(typed) => {
                const verdict = commit(key, typed, rules[key] ?? {});
                // The row is keyed by its host, so a rename remounts it under
                // the new name; without this the editor would close on itself.
                if (verdict === 'ok') setOpenRow(storedId(normalizeHost(typed)));
                return verdict;
              }}
              onChange={(next) => write({ ...rules, [key]: next })}
              onRemove={() => {
                const map = { ...rules };
                delete map[key];
                write(map);
              }}
            />
          ))}
          {pending.map((row, i) => (
            <HostRuleRow
              key={row.id}
              host={row.host}
              rule={row.rule}
              index={keys.length + i}
              last={keys.length + i === rowCount - 1}
              open={openRow === row.id}
              onToggle={() => setOpenRow(openRow === row.id ? '' : row.id)}
              onCommit={(typed) => {
                // Kept even when it cannot be stored yet, so collapsing a row
                // whose host is still half typed does not throw the text away.
                setPending((p) => p.map((r) => (r.id === row.id ? { ...r, host: typed } : r)));
                const verdict = commit('', typed, row.rule);
                if (verdict === 'ok') {
                  setPending((p) => p.filter((r) => r.id !== row.id));
                  setOpenRow(storedId(normalizeHost(typed)));
                }
                return verdict;
              }}
              onChange={(next) => setPending((p) => p.map((r) => (r.id === row.id ? { ...r, rule: next } : r)))}
              onRemove={() => setPending((p) => p.filter((r) => r.id !== row.id))}
            />
          ))}
        </ul>
      )}
    </Card>
  );
}

/**
 * One host, collapsed to its name and its numbers, expanded to the six fields.
 *
 * The typed host lives HERE and reaches the draft only on blur. Per keystroke
 * it would be a new map key per keystroke, and the draft would end up holding
 * every prefix of the name as a row of its own.
 */
function HostRuleRow({
  host,
  rule,
  index,
  last,
  open,
  onToggle,
  onCommit,
  onChange,
  onRemove,
}: {
  host: string;
  rule: HostRule;
  index: number;
  last: boolean;
  open: boolean;
  onToggle: () => void;
  onCommit: (typed: string) => Verdict;
  onChange: (next: HostRule) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  const [text, setText] = useState(host);
  const [duplicate, setDuplicate] = useState(false);

  const retry: RetryRule = rule.retry ?? {};
  const never = retry.never ?? false;
  const delay = retry.delay ?? 0;
  const ceiling = retry.max ?? 0;
  const setRetry = (fields: Partial<RetryRule>) => onChange({ ...rule, retry: { ...retry, ...fields } });

  // What the ceiling will really be, on the rows where it disagrees with the
  // wait. RetryFor (settings_hostrules.go) raises plan.Max to plan.Delay while
  // it resolves one failure and never writes that back, so this is NOT a
  // save-time clamp: the stored 60 stays 60 and behaves as 3600 for good. Both
  // numbers have to be set for the raise to be certain - a 0 falls through to a
  // level this page cannot see, and guessing what it will find there would be
  // inventing a number. Left out while "never" is on, where no wait is ever
  // used at all.
  const raisedTo = !never && delay > 0 && ceiling > 0 && ceiling < delay ? delay : undefined;

  const commit = () => {
    const verdict = onCommit(text);
    setDuplicate(verdict === 'duplicate');
    // A name the server would throw away is not written at all. For a stored
    // row the box goes back to the name it still has, rather than leaving
    // something on screen that a save would silently delete; a new row simply
    // stays where it is, unsaved, until it has a host.
    if (verdict === 'blank' && host !== '') setText(host);
  };

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
          {/* dir=ltr: a host name is never read right to left, whatever the
              interface language is. */}
          <span dir="ltr" className="min-w-0 flex-1 truncate text-sm text-carbon-text">
            {host || <span className="text-carbon-textMuted">{t('settings.hostRules.pattern')}</span>}
          </span>
          {never && (
            <span className="hidden truncate text-xs text-statusWarn md:block md:max-w-[12rem]">
              {t('settings.hostRules.never')}
            </span>
          )}
          {/* What this row actually overrides, in the order the editor asks for
              it, each number behind the glyph of the field it came from. The
              glyphs and not a word apiece: three headings would need three more
              strings for a strip that is read at a glance and explained in full
              one click away, and a native `title` tooltip is not this app's way
              of explaining anything (every hover explanation is a GlimStone
              bubble). A zero prints as nothing at all - it is the row saying
              nothing about that number, and a column of noughts would read as
              "this hoster gets none", the one thing 0 never means here. */}
          <Summary value={rule.maxPerHost} icon={<IconDownloads width={12} height={12} />} />
          <Summary value={rule.chunks} icon={<IconBolt width={12} height={12} />} />
          <Summary value={never ? 0 : retry.tries} icon={<IconRetry width={12} height={12} />} />
        </button>
        {/* The one row action, on hover and on keyboard focus, so a long table
            reads as content rather than as a wall of buttons. */}
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            kind="danger"
            icon={<IconTrash width={14} height={14} />}
            hue={index}
            title={t('settings.hostRules.remove')}
            aria-label={t('settings.hostRules.remove')}
            // Keeping focus in the host box means no blur, and therefore no
            // commit, in front of this click. Without it a new row whose host
            // was just typed would be written to the draft on the way out and
            // this press would then delete a row that no longer exists.
            onMouseDown={(e) => e.preventDefault()}
            onClick={onRemove}
          />
        </div>
      </div>

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          <Field label={t('settings.hostRules.pattern')} hint={t('settings.hostRules.patternHint')}>
            <TextInput
              dir="ltr"
              spellCheck={false}
              value={text}
              placeholder="rapidgator.net"
              onChange={(e) => {
                setText(e.target.value);
                setDuplicate(false);
              }}
              onBlur={commit}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  e.currentTarget.blur();
                }
              }}
            />
          </Field>
          {/* The state of this row, not an explanation of the field - the
              explanation is behind the (i) on the label. Refused rather than
              stored, because two keys that normalise the same both survive the
              save and only one of them is ever consulted. */}
          {duplicate && <p className="text-xs text-statusWarn">{t('settings.hostRules.duplicate')}</p>}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t('settings.hostRules.maxPerHost')} hint={t('settings.hostRules.maxPerHostHint')}>
              <NumberInput
                value={rule.maxPerHost ?? 0}
                min={0}
                max={MAX_PER_HOST}
                onValue={(v) => onChange({ ...rule, maxPerHost: clampInt(v, MAX_PER_HOST) })}
              />
            </Field>
            {/* An override and not a ceiling: this may be HIGHER than the global
                count, unlike a limit a resolver reports about the host, which
                can only lower it. */}
            <Field label={t('settings.hostRules.chunks')} hint={t('settings.hostRules.chunksHint')}>
              <NumberInput
                value={rule.chunks ?? 0}
                min={0}
                max={MAX_CHUNKS}
                onValue={(v) => onChange({ ...rule, chunks: clampInt(v, MAX_CHUNKS) })}
              />
            </Field>
          </div>

          <ToggleRow
            hue={index}
            checked={never}
            onChange={(v) => setRetry({ never: v })}
            label={t('settings.hostRules.never')}
            hint={t('settings.hostRules.neverHint')}
          />

          {/* Dimmed and disabled while "never" is on, because RetryFor reads
              none of these three once it wins - but deliberately NOT wrapped in
              pointer-events-none: that would take the (i) bubbles with it, and
              the bubble is the one place that explains why the fields are dim.
              Off here does not clear a "never" a level below has already set:
              the flag merges with OR, so this switch can only ever turn one on. */}
          <div className={`grid grid-cols-1 gap-4 sm:grid-cols-3 ${never ? 'opacity-40' : ''}`}>
            <RetryNumber
              label={t('settings.hostRules.retryDelay')}
              hint={t('settings.hostRules.retryDelayHint')}
              value={delay}
              max={MAX_WAIT}
              off={never}
              onValue={(v) => setRetry({ delay: v })}
            />
            {/* A ceiling below the first wait is not cut at save time and not
                rewritten here either - the field keeps the number somebody
                meant to type. What it gets instead is the number that will
                actually be in force, so the row does not quietly disagree with
                itself; the (i) says why in words. */}
            <RetryNumber
              label={t('settings.hostRules.retryMax')}
              hint={t('settings.hostRules.retryMaxHint')}
              value={ceiling}
              max={MAX_WAIT}
              off={never}
              raisedTo={raisedTo}
              onValue={(v) => setRetry({ max: v })}
            />
            <RetryNumber
              label={t('settings.hostRules.retryTries')}
              hint={t('settings.hostRules.retryTriesHint')}
              value={retry.tries ?? 0}
              max={MAX_TRIES}
              off={never}
              onValue={(v) => setRetry({ tries: v })}
            />
          </div>
        </div>
      )}
    </li>
  );
}

/**
 * One of the three retry numbers, switched off by "never".
 *
 * `disabled` reaches the input itself, and pointer-events are taken off the
 * control as well, because NumberInput's up and down arrows are separate
 * buttons that do not read the input's disabled state - without this the value
 * could still be clicked up on a row that never retries at all. The block sits
 * around the CONTROL and never around the caption: the (i) beside the label is
 * the one thing that explains why the field is dim, so it has to stay hoverable
 * while it is.
 *
 * `raisedTo` is the seconds this field will really be worth when the stored
 * number is not the one that gets used. It is shown as the bare figure in the
 * warning colour rather than as a sentence: the sentence is already in the (i)
 * beside the label, in every language, and a second one written here could only
 * be written in English.
 */
function RetryNumber({
  label,
  hint,
  value,
  max,
  off,
  raisedTo,
  onValue,
}: {
  label: string;
  hint: string;
  value: number;
  max: number;
  off: boolean;
  raisedTo?: number;
  onValue: (n: number) => void;
}) {
  return (
    <Field label={label} hint={hint}>
      <span className={`block ${off ? 'pointer-events-none' : ''}`}>
        <NumberInput value={value} min={0} max={max} disabled={off} onValue={(v) => onValue(clampInt(v, max))} />
        {raisedTo !== undefined && (
          <span className="mt-1 flex items-center gap-1 text-xs text-statusWarn">
            <IconWarning width={12} height={12} />
            <span className="glim-num">{raisedTo}</span>
          </span>
        )}
      </span>
    </Field>
  );
}

/** One number from the collapsed row, behind the glyph of the field it came
 *  from. The chip keeps its width while it is empty, so the numbers stay in
 *  their columns down a long table instead of sliding about row by row. */
function Summary({ value, icon }: { value?: number; icon: ReactNode }) {
  return (
    <span className="hidden w-12 shrink-0 items-center justify-end gap-1 text-xs text-carbon-textMuted sm:flex">
      {value ? (
        <>
          {icon}
          <span className="glim-num">{value}</span>
        </>
      ) : null}
    </span>
  );
}
