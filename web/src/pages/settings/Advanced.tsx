import { Fragment, useEffect, useMemo, useState } from 'react';
import { fetchOptions, type ApiOptions, type Settings } from '../../lib/api';
import type { TranslationKey } from '../../lib/i18n';
import { reasonKey } from '../../components/columns';
import { Dropdown, type DropdownOption } from '../../components/Dropdown';
import {
  Card,
  FieldGroup,
  IconBadge,
  InfoBubble,
  NumberInput,
  SectionTitle,
  TextArea,
  TextInput,
} from '../../components/ui';
import { PathInput } from '../../components/FolderPicker';
import { IconRetry, IconSearch } from '../../lib/icons';
import { PATH_KEYS } from '../../lib/settingsTransfer';
import { COLLISION_LABEL, DISPOSAL_LABEL } from './Archives';
import { CONFIRM_LABEL } from './collector/Collector';
import { useDraft, useFieldError } from './context';
import { NeutralSwitch } from './controls';
import { fetchSettingsSchema, type SettingsSchema } from './features';
import { getPath, rowsFor, same, setPath, type Row, type ValueKind } from './paths';
import { label, useTx, type ChoicePrefix } from './tx';

/**
 * Advanced lists every setting by key, generated from the settings document so
 * new fields appear without an edit here. It edits the shared draft, so the
 * server's sanitize and validate still apply. Above the table sits a card for
 * settings that have no other page but need an explanation; their keys keep
 * their raw rows in the table as well, and since both edit the same draft the
 * two cannot disagree.
 */

// Output that rides along in the same response, not settings.
const NOT_SETTINGS = ['problems'] as const;

// The mask for a stored reconnect password, so the row can explain it.
const REDACTED = '********';

/**
 * The failure words the retry table is keyed on, taken from columns.tsx's
 * reasonKey so the table and the task list name and order them alike.
 */
const RETRY_REASONS = Object.entries(reasonKey);

// settings.maxRetryTries; sanitizeRetryRule cuts anything above it on save.
const MAX_TRIES = 20;

type Namer = (tx: (key: TranslationKey) => string, id: string) => string;

const fromMap =
  (labels: Partial<Record<string, TranslationKey>>): Namer =>
  (tx, id) => {
    const key = labels[id];
    return key ? tx(key) : id;
  };

const fromPrefix =
  (prefix: ChoicePrefix): Namer =>
  (tx, id) =>
    label(tx, prefix, id);

type ChoiceList = {
  [K in keyof ApiOptions]-?: ApiOptions[K] extends string[] ? K : never;
}[keyof ApiOptions];

/**
 * The settings whose values GET /api/options lists, each with the list and the
 * words its own page uses. Their rows pick from a menu: sanitize folds a value
 * it does not know back to the default, so a typo in a text field would be
 * saved as something nobody chose.
 */
const CHOICE_ROWS: Partial<Record<string, { list: ChoiceList; name: Namer }>> = {
  extractCollision: { list: 'archiveCollisions', name: fromMap(COLLISION_LABEL) },
  archiveDisposal: { list: 'archiveDisposals', name: fromMap(DISPOSAL_LABEL) },
  collisionPolicy: { list: 'collisionPolicies', name: fromMap(COLLISION_LABEL) },
  mirrorPolicy: { list: 'mirrorPolicies', name: fromPrefix('settings.advanced.mirror.') },
  onDupes: { list: 'confirmPolicies', name: fromMap(CONFIRM_LABEL) },
  onOffline: { list: 'confirmPolicies', name: fromPrefix('settings.advanced.offline.') },
  reclaimTrust: { list: 'reclaimTrustModes', name: fromPrefix('settings.advanced.reclaim.') },
  resumeOnStart: { list: 'resumeModes', name: fromPrefix('settings.resume.') },
};

export function Advanced() {
  const { tx } = useTx();
  const { cfg, patch, replace } = useDraft();
  const doc = cfg as unknown as Record<string, unknown>;

  const [schema, setSchema] = useState<SettingsSchema | null>(null);
  const [schemaFailed, setSchemaFailed] = useState(false);
  // Until the lists arrive, or when they do not, those rows are text fields.
  const [options, setOptions] = useState<ApiOptions | null>(null);
  const [query, setQuery] = useState('');
  const [searchOpen, setSearchOpen] = useState(false);
  // Debounced, since the filter re-renders every visible editor.
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
    fetchOptions().then(
      (o) => live && setOptions(o),
      () => {},
    );
    return () => {
      live = false;
    };
  }, []);

  const rows = useMemo(() => rowsFor(doc, schema?.kinds ?? {}, NOT_SETTINGS), [doc, schema]);

  function choicesFor(path: string): DropdownOption[] | undefined {
    const row = CHOICE_ROWS[path];
    const ids = row && options?.[row.list];
    if (!row || !ids?.length) return undefined;
    return ids.map((id) => ({ value: id, label: row.name(tx, id) }));
  }

  const shown = useMemo(() => {
    return rows.filter((r) => {
      if (onlyModified && (!schema || same(r.value, getPath(schema.values, r.path)))) return false;
      if (!needle) return true;
      // The value is searched too. `?? null` matters: JSON.stringify(undefined)
      // is undefined, which every key dropped by omitempty would hit.
      const text = JSON.stringify(r.value ?? null) ?? 'null';
      return r.path.toLowerCase().includes(needle) || text.toLowerCase().includes(needle);
    });
  }, [rows, needle, onlyModified, schema]);

  function write(path: string, value: unknown) {
    replace(setPath(doc, path, value) as unknown as Settings);
  }

  // Go encodes an empty map as null, and an older server sends no retry at all.
  const rules = cfg.retry?.byReason ?? {};

  /**
   * setTries writes one failure's attempt count. A 0 removes the rule, since
   * omitempty makes absent and 0 the same, and retry.delay and retry.max are
   * left alone.
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
      <Card hue={0} className="flex flex-col gap-5">
        <SectionTitle>{tx('settings.advanced.retryTitle')}</SectionTitle>
        {/* One bubble for the grid: 0 defers to the count on the Downloads
            page, and a host rule beats this, which beats that count. */}
        <FieldGroup label={tx('settings.advanced.retryTries')} hint={tx('settings.advanced.retryTriesHint')}>
          {/* Scrolls in its own box, so the page keeps the rail and save bar. */}
          <div className="overflow-x-auto">
            <div className="grid min-w-[18rem] grid-cols-[1fr_auto] items-center gap-x-4 gap-y-2">
              {RETRY_REASONS.map(([reason, labelKey]) => (
                <Fragment key={reason}>
                  <span className="text-sm text-carbon-text">{tx(labelKey)}</span>
                  {/* The spinner is named by the failure's word; max is the
                      server's ceiling. */}
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

      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-3">
          {/* Needs the factory values to compare against. */}
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
          {/* A badge that opens into the field and stays open while it holds
              text, so an active filter is never hidden. */}
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

        <Card hue={1} padding="none">
          <div className="p-5 pb-0">
            <SectionTitle>{tx('settings.advanced.allSettings')}</SectionTitle>
          </div>
          {/* Scrolls in its own box, so the page keeps the rail and save bar. */}
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
                    choices={choicesFor(r.path)}
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
  choices,
  onWrite,
}: {
  row: Row;
  hue: number;
  fallback: unknown;
  canReset: boolean;
  choices?: DropdownOption[];
  onWrite: (path: string, value: unknown) => void;
}) {
  const { tx } = useTx();
  const modified = canReset && !same(row.value, fallback);
  const secret = row.value === REDACTED;
  // A list row edits the whole list, so a refusal of one of its rows shows here.
  const refused = useFieldError(row.path, true);

  return (
    <div className="flex flex-col gap-2 px-4 py-3 odd:bg-carbon-surface2/30 sm:flex-row sm:items-center sm:gap-4">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex items-center">
          {/* A key path stays left to right, or RTL would move the dots. */}
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

      <div className="flex w-full flex-col gap-1 sm:w-72">
        <ValueEditor row={row} hue={hue} choices={choices} refused={refused} onWrite={onWrite} />
        {refused && !PATH_KEYS.includes(row.path) && <span className="text-xs text-statusWarn">{refused}</span>}
      </div>

      {/* Reset shows only where there is something to undo. */}
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
  choices,
  refused,
  onWrite,
}: {
  row: Row;
  hue: number;
  choices?: DropdownOption[];
  /** Why the server refused the value; a folder field shows it itself. */
  refused?: string;
  onWrite: (path: string, value: unknown) => void;
}) {
  const { tx } = useTx();
  // Edited as text and written back only once it parses.
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
    default: {
      const value = row.value === null || row.value === undefined ? '' : String(row.value);
      if (choices) {
        return <Dropdown label={row.path} value={value} options={choices} onChange={(v) => onWrite(row.path, v)} />;
      }
      if (PATH_KEYS.includes(row.path)) {
        return <PathInput label={row.path} value={value} error={refused} onValue={(v) => onWrite(row.path, v)} />;
      }
      return (
        <TextInput dir="ltr" spellCheck={false} value={value} onChange={(e) => onWrite(row.path, e.target.value)} />
      );
    }
  }
}

/**
 * emptyFor is the reset value for a key the defaults leave out. It is the zero
 * value of the key's kind, since undefined would drop the key from the
 * document PUT replaces wholesale.
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
