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

// Per-host exceptions to the global counts: how many downloads one hoster may
// have open, how many connections each gets, and how a failure is retried.
//
// settings.hostRules is a map keyed by the host pattern, so a row is written
// only on blur and only with a name (sanitizeHostRules drops a blank one), and
// a rename is delete-then-set. Zero means "no opinion" on every number and
// falls through to the level below.

// The server's ceilings; it cuts anything above them on save.
const MAX_PER_HOST = 64; // maxConcurrentCeiling
const MAX_CHUNKS = 16; // rules.MaxChunks
const MAX_TRIES = 20; // maxRetryTries
const MAX_WAIT = 86400; // maxRetryWait, one day in seconds

/**
 * normalizeHost mirrors the Go normalizeHostPattern. Two keys that normalise
 * the same would both be stored with only one ever consulted, so the editor
 * refuses the second.
 */
function normalizeHost(raw: string): string {
  const trimmed = raw.trim().toLowerCase();
  const bare = trimmed.startsWith('*.') ? trimmed.slice(2) : trimmed;
  return bare.replace(/^\.+/, '').replace(/\.+$/, '');
}

/** clampInt cuts like the server does, so the box shows what the save stores. */
function clampInt(v: number, max: number): number {
  if (!Number.isFinite(v)) return 0;
  return Math.min(Math.max(0, Math.round(v)), max);
}

/** What committing a typed host did, so the row can say why nothing happened. */
type Verdict = 'ok' | 'blank' | 'duplicate';

/** A new row without a host, kept in component state until it has a map key. */
interface PendingRow {
  id: string;
  host: string;
  rule: HostRule;
}

let pendingCounter = 0;
const freshId = () => `p${(pendingCounter++).toString(36)}`;

// Row ids for stored and new rows, kept apart by prefix since a host can be
// anything somebody types.
const storedId = (key: string) => `k:${key}`;

export function HostRulesCard({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();

  // An older settings.json sends null.
  const rules = cfg.hostRules ?? {};
  // Sorted by raw key, the order in which the server settles ties.
  const keys = Object.keys(rules).sort();

  const [openRow, setOpenRow] = useState('');
  const [pending, setPending] = useState<PendingRow[]>([]);

  const write = (next: Record<string, HostRule>) => patch({ hostRules: next });

  // Moves `rule` under the typed host, refusing a blank name or one that would
  // shadow an existing row.
  const commit = (fromKey: string, typed: string, rule: HostRule): Verdict => {
    const next = normalizeHost(typed);
    if (next === '') return 'blank';
    // An identical patch would still mark the draft dirty.
    if (next === fromKey) return 'ok';
    if (next !== normalizeHost(fromKey) && keys.some((k) => k !== fromKey && normalizeHost(k) === next)) {
      return 'duplicate';
    }
    const map = { ...rules };
    delete map[fromKey];
    map[next] = rule;
    write(map);
    return 'ok';
  };

  const add = () => {
    // A nameless row already waiting is opened instead of adding another.
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
        // Inside the card rather than an EmptyState, which would hide Add.
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
                // Kept so collapsing a half-typed row keeps the text.
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
 * HostRuleRow shows one host, collapsed to its name and numbers, expanded to the
 * six fields. The typed host reaches the draft only on blur, or every prefix
 * would become a map key.
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

  // RetryFor raises a ceiling below the first wait to that wait at run time
  // without storing it. Shown only when both numbers are set here.
  const raisedTo = !never && delay > 0 && ceiling > 0 && ceiling < delay ? delay : undefined;

  const commit = () => {
    const verdict = onCommit(text);
    setDuplicate(verdict === 'duplicate');
    // A stored row cleared to blank gets its name back; a new row waits.
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
          <span dir="ltr" className="min-w-0 flex-1 truncate text-sm text-carbon-text">
            {host || <span className="text-carbon-textMuted">{t('settings.hostRules.pattern')}</span>}
          </span>
          {never && (
            <span className="hidden truncate text-xs text-statusWarn md:block md:max-w-[12rem]">
              {t('settings.hostRules.never')}
            </span>
          )}
          {/* The overrides behind their fields' glyphs. A zero prints nothing,
              since it means "no opinion", not "none". */}
          <Summary value={rule.maxPerHost} icon={<IconDownloads width={12} height={12} />} />
          <Summary value={rule.chunks} icon={<IconBolt width={12} height={12} />} />
          <Summary value={never ? 0 : retry.tries} icon={<IconRetry width={12} height={12} />} />
        </button>
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            // A lone glyph takes half its 32px badge.
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={t('settings.hostRules.remove')}
            aria-label={t('settings.hostRules.remove')}
            // No blur, so no commit happens in front of the removal.
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
          {/* Refused, since two keys that normalise the same would both be
              stored and only one consulted. */}
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
            {/* An override, so it may exceed the global count. */}
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

          {/* Absent while "never" is on, since RetryFor then reads none of
              them. The flag merges with OR, so switching it off here cannot
              clear a "never" set a level below. */}
          {!never && (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
              <RetryNumber
                label={t('settings.hostRules.retryDelay')}
                hint={t('settings.hostRules.retryDelayHint')}
                value={delay}
                max={MAX_WAIT}
                onValue={(v) => setRetry({ delay: v })}
              />
              {/* The typed ceiling stays; the number in force is shown beside it. */}
              <RetryNumber
                label={t('settings.hostRules.retryMax')}
                hint={t('settings.hostRules.retryMaxHint')}
                value={ceiling}
                max={MAX_WAIT}
                raisedTo={raisedTo}
                onValue={(v) => setRetry({ max: v })}
              />
              <RetryNumber
                label={t('settings.hostRules.retryTries')}
                hint={t('settings.hostRules.retryTriesHint')}
                value={retry.tries ?? 0}
                max={MAX_TRIES}
                onValue={(v) => setRetry({ tries: v })}
              />
            </div>
          )}
        </div>
      )}
    </li>
  );
}

/**
 * RetryNumber is one of the three retry numbers. `raisedTo` is the value in
 * force when it differs from the stored one, shown as a bare figure since the
 * (i) explains it.
 */
function RetryNumber({
  label,
  hint,
  value,
  max,
  raisedTo,
  onValue,
}: {
  label: string;
  hint: string;
  value: number;
  max: number;
  raisedTo?: number;
  onValue: (n: number) => void;
}) {
  return (
    <Field label={label} hint={hint}>
      <span className="block">
        <NumberInput value={value} min={0} max={max} onValue={(v) => onValue(clampInt(v, max))} />
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

/** Summary keeps its width when empty, so the numbers stay in their columns. */
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
