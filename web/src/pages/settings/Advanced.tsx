import { useEffect, useMemo, useState } from 'react';
import type { Settings } from '../../lib/api';
import { Button, Card, InfoBubble, NumberInput, SectionTitle, TextArea, TextInput } from '../../components/ui';
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
 */

// Not settings, and so not editable: the rule-compile problems ride along in the
// same response as output. A reset button on them would be a control with
// nothing behind it.
const NOT_SETTINGS = ['problems'] as const;

// What the reconnect package puts in place of a stored password. Matching it is
// how the row knows to say "leave this alone to keep the stored one" instead of
// showing eight asterisks with no explanation.
const REDACTED = '********';

export function Advanced() {
  const { tx } = useTx();
  const { cfg, replace } = useDraft();
  const doc = cfg as unknown as Record<string, unknown>;

  const [schema, setSchema] = useState<SettingsSchema | null>(null);
  const [schemaFailed, setSchemaFailed] = useState(false);
  const [query, setQuery] = useState('');
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

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1 sm:max-w-xs">
          <TextInput
            type="search"
            spellCheck={false}
            value={query}
            placeholder={tx('settings.advanced.search')}
            aria-label={tx('settings.advanced.search')}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
        {/* Disabled until the factory values arrive: "only what differs from the
            default" with no defaults to compare against would answer "all of
            it", which is the opposite of what it says. */}
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
      </div>

      <Card className="p-0">
        <div className="p-5 pb-0">
          <SectionTitle hue={0}>{tx('settings.advanced.allSettings')}</SectionTitle>
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
          not a wall of buttons on a page that is already dense. */}
      <div className="w-16 shrink-0">
        {modified && (
          <Button
            kind="ghost"
            className="px-2 py-1 text-[11px]"
            title={tx('settings.advanced.resetTitle')}
            onClick={() => onWrite(row.path, fallback ?? emptyFor(row.kind))}
          >
            {tx('settings.advanced.reset')}
          </Button>
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
          onLabel="on"
          offLabel="off"
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
