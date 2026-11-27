import { useEffect, useId, useState, type KeyboardEvent } from 'react';
import { FieldError, NumberInput, TextInput, ToggleRow } from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import type { FeedBackfill, FeedMediathek, FeedSubscription } from '../../../lib/api';
import { useT, type TranslationKey } from '../../../lib/i18n';
import { SettingRow } from '../controls';

// The choices of what a subscription takes, apart from the feed itself. Each
// field holds back what the server would refuse, since one refused row fails
// the whole settings document.

/** Where a count or a span of days starts when it is first chosen. */
const BACKFILL_START: Record<'count' | 'days', number> = { count: 20, days: 30 };

/**
 * compiles catches a pattern JavaScript cannot parse. RE2 refuses more than
 * RegExp, so a pattern that passes may still be refused at save, but nothing
 * the server accepts is blocked here.
 */
export function compiles(pattern: string): boolean {
  try {
    new RegExp(pattern);
    return true;
  } catch {
    return false;
  }
}

const blurOnEnter = (e: KeyboardEvent<HTMLInputElement>) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    e.currentTarget.blur();
  }
};

/**
 * PatternField is a regular expression committed on blur, and only once it
 * compiles. Undefined goes out for an empty field, which is how the row says
 * it has no pattern.
 */
export function PatternField({
  label,
  hint,
  value,
  onValue,
}: {
  label: string;
  hint: string;
  value: string | undefined;
  onValue: (next: string | undefined) => void;
}) {
  const { t } = useT();
  const [text, setText] = useState(value ?? '');
  const [bad, setBad] = useState(false);
  const reasonId = useId();

  // The save answer replaces the draft and may trim the pattern, so follow it.
  useEffect(() => {
    setText(value ?? '');
    setBad(false);
  }, [value]);

  const commit = () => {
    const next = text.trim();
    // An identical patch would still mark the draft dirty.
    if (next === (value ?? '')) {
      setBad(false);
      return;
    }
    if (next !== '' && !compiles(next)) {
      setBad(true);
      return;
    }
    setBad(false);
    onValue(next === '' ? undefined : next);
  };

  return (
    <SettingRow control="long" label={label} hint={hint}>
      <TextInput
        dir="ltr"
        spellCheck={false}
        value={text}
        aria-invalid={bad}
        aria-describedby={bad ? reasonId : undefined}
        onChange={(e) => {
          setText(e.target.value);
          setBad(false);
        }}
        onBlur={commit}
        onKeyDown={blurOnEnter}
      />
      {bad && <FieldError id={reasonId}>{t('settings.feeds.filterInvalid')}</FieldError>}
    </SettingRow>
  );
}

/** The two length bounds, each 0 for none, held back while the shortest is above the longest. */
export function LengthFields({ row, onChange }: { row: FeedSubscription; onChange: (next: FeedSubscription) => void }) {
  const { t } = useT();
  const [min, setMin] = useState(row.minMinutes ?? 0);
  const [max, setMax] = useState(row.maxMinutes ?? 0);
  const minReasonId = useId();
  const maxReasonId = useId();
  // The field just edited carries the reason, so it stands where the eye is.
  const [blame, setBlame] = useState<'min' | 'max'>('min');

  useEffect(() => setMin(row.minMinutes ?? 0), [row.minMinutes]);
  useEffect(() => setMax(row.maxMinutes ?? 0), [row.maxMinutes]);

  const crossed = (lo: number, hi: number) => lo > 0 && hi > 0 && lo > hi;
  const set = (lo: number, hi: number) => {
    const bound = (v: number) => (Number.isFinite(v) ? Math.max(0, Math.round(v)) : 0);
    const next = { lo: bound(lo), hi: bound(hi) };
    setMin(next.lo);
    setMax(next.hi);
    if (crossed(next.lo, next.hi)) return;
    const out = { ...row };
    delete out.minMinutes;
    delete out.maxMinutes;
    if (next.lo > 0) out.minMinutes = next.lo;
    if (next.hi > 0) out.maxMinutes = next.hi;
    onChange(out);
  };

  const wrong = crossed(min, max);

  return (
    <>
      <SettingRow control="short" label={t('settings.feeds.minMinutes')} hint={t('settings.feeds.minMinutesHint')}>
        <NumberInput
          value={min}
          min={0}
          step={1}
          aria-invalid={wrong && blame === 'min'}
          aria-describedby={wrong && blame === 'min' ? minReasonId : undefined}
          onValue={(v) => {
            setBlame('min');
            set(v, max);
          }}
        />
        {wrong && blame === 'min' && <FieldError id={minReasonId}>{t('settings.feeds.minAboveMax')}</FieldError>}
      </SettingRow>
      <SettingRow control="short" label={t('settings.feeds.maxMinutes')} hint={t('settings.feeds.maxMinutesHint')}>
        <NumberInput
          value={max}
          min={0}
          step={1}
          aria-invalid={wrong && blame === 'max'}
          aria-describedby={wrong && blame === 'max' ? maxReasonId : undefined}
          onValue={(v) => {
            setBlame('max');
            set(min, v);
          }}
        />
        {wrong && blame === 'max' && <FieldError id={maxReasonId}>{t('settings.feeds.maxBelowMin')}</FieldError>}
      </SettingRow>
    </>
  );
}

type BackfillMode = 'none' | FeedBackfill['mode'];

const BACKFILL_LABEL: Record<BackfillMode, TranslationKey> = {
  none: 'settings.feeds.backfillNone',
  all: 'settings.feeds.backfillAll',
  count: 'settings.feeds.backfillCount',
  days: 'settings.feeds.backfillDays',
};

/** BackfillRow chooses what a channel's first check hands over besides what is new. */
export function BackfillRow({ row, onChange }: { row: FeedSubscription; onChange: (next: FeedSubscription) => void }) {
  const { t } = useT();
  const mode: BackfillMode = row.backfill?.mode ?? 'none';

  const choose = (next: BackfillMode) => {
    const out = { ...row };
    delete out.backfill;
    if (next === 'all') out.backfill = { mode: 'all' };
    if (next === 'count' || next === 'days') out.backfill = { mode: next, n: BACKFILL_START[next] };
    onChange(out);
  };

  return (
    <>
      <SettingRow label={t('settings.feeds.backfill')} hint={t('settings.feeds.backfillHint')}>
        <Tabs
          variant="well"
          size="sm"
          inline
          hug
          label={t('settings.feeds.backfill')}
          active={mode}
          onSelect={(id) => choose(id as BackfillMode)}
          items={(['none', 'all', 'count', 'days'] as const).map((m) => ({ id: m, label: t(BACKFILL_LABEL[m]) }))}
        />
      </SettingRow>
      {(mode === 'count' || mode === 'days') && (
        <SettingRow
          control="short"
          label={t(mode === 'count' ? 'settings.feeds.backfillCountN' : 'settings.feeds.backfillDaysN')}
        >
          <NumberInput
            value={row.backfill?.n ?? BACKFILL_START[mode]}
            min={1}
            step={1}
            onValue={(v) => {
              // The server refuses anything below 1.
              const n = Number.isFinite(v) ? Math.max(1, Math.round(v)) : 1;
              onChange({ ...row, backfill: { mode, n } });
            }}
          />
        </SettingRow>
      )}
    </>
  );
}

type NameField = 'channel' | 'topic' | 'title';

const NAME_FIELDS: { id: NameField; label: TranslationKey; hint: TranslationKey; placeholder: string }[] = [
  { id: 'channel', label: 'settings.feeds.mediathekChannel', hint: 'settings.feeds.mediathekChannelHint', placeholder: 'ARD' },
  { id: 'topic', label: 'settings.feeds.mediathekTopic', hint: 'settings.feeds.mediathekTopicHint', placeholder: 'Tatort' },
  { id: 'title', label: 'settings.feeds.mediathekTitle', hint: 'settings.feeds.mediathekTitleHint', placeholder: '' },
];

const QUALITY_LABEL: Record<FeedMediathek['quality'], TranslationKey> = {
  hd: 'settings.feeds.mediathekQualityHd',
  normal: 'settings.feeds.mediathekQualityNormal',
  low: 'settings.feeds.mediathekQualityLow',
};

export const NO_MEDIATHEK: FeedMediathek = { channel: '', topic: '', title: '', quality: 'hd', subtitles: false };

/**
 * MediathekFields is the search a mediathek subscription runs. The names are
 * committed on blur, and an edit that would empty all three is held back,
 * since a search for nothing would take every programme there is.
 */
export function MediathekFields({ row, onChange }: { row: FeedSubscription; onChange: (next: FeedSubscription) => void }) {
  const { t } = useT();
  const search = row.mediathek ?? NO_MEDIATHEK;
  const [text, setText] = useState<Record<NameField, string>>(search);
  const [empty, setEmpty] = useState<NameField | ''>('');
  const reasonId = useId();

  useEffect(() => {
    setText({ channel: search.channel, topic: search.topic, title: search.title });
    setEmpty('');
  }, [search.channel, search.topic, search.title]);

  const commit = (id: NameField) => {
    const next = { ...search, channel: text.channel.trim(), topic: text.topic.trim(), title: text.title.trim() };
    if (next[id] === search[id]) {
      setEmpty('');
      return;
    }
    if (!next.channel && !next.topic && !next.title) {
      setEmpty(id);
      return;
    }
    setEmpty('');
    onChange({ ...row, mediathek: next });
  };

  return (
    <>
      {NAME_FIELDS.map((f) => (
        <SettingRow key={f.id} control="long" label={t(f.label)} hint={t(f.hint)}>
          <TextInput
            value={text[f.id]}
            placeholder={f.placeholder}
            spellCheck={false}
            aria-invalid={empty === f.id}
            aria-describedby={empty === f.id ? reasonId : undefined}
            onChange={(e) => {
              const value = e.target.value;
              setText((all) => ({ ...all, [f.id]: value }));
              setEmpty('');
            }}
            onBlur={() => commit(f.id)}
            onKeyDown={blurOnEnter}
          />
          {empty === f.id && <FieldError id={reasonId}>{t('settings.feeds.mediathekEmpty')}</FieldError>}
        </SettingRow>
      ))}
      <SettingRow label={t('settings.feeds.mediathekQuality')} hint={t('settings.feeds.mediathekQualityHint')}>
        <Tabs
          variant="well"
          size="sm"
          inline
          hug
          label={t('settings.feeds.mediathekQuality')}
          active={search.quality}
          onSelect={(id) => onChange({ ...row, mediathek: { ...search, quality: id as FeedMediathek['quality'] } })}
          items={(['hd', 'normal', 'low'] as const).map((q) => ({ id: q, label: t(QUALITY_LABEL[q]) }))}
        />
      </SettingRow>
      <ToggleRow
        checked={search.subtitles}
        onChange={(v) => onChange({ ...row, mediathek: { ...search, subtitles: v } })}
        label={t('settings.feeds.mediathekSubtitles')}
        hint={t('settings.feeds.mediathekSubtitlesHint')}
      />
    </>
  );
}
