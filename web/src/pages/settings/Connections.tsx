import { useCallback, useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  Field,
  IconBadge,
  Modal,
  NumberInput,
  SectionTitle,
  TextArea,
  TextInput,
} from '../../components/ui';
import { IconArrowDown, IconArrowUp, IconGlobe, IconPlus, IconTrash } from '../../lib/icons';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';
import { NeutralSwitch } from './controls';

/**
 * The connection manager: the ordered list of outbound connections downloads are
 * spread across, which JDownloader calls the Verbindungsverwaltung.
 *
 * Three things about this page are decisions rather than layout.
 *
 * The list is part of the SETTINGS DRAFT, not a resource of its own. It is a
 * field of the settings document, PUT /api/settings already validates every row
 * and merges back the passwords the client was never shown, and a second write
 * path would be a second place to forget that merge — where forgetting it means
 * every proxy password cleared on the next save, noticed days later when a
 * download fails.
 *
 * A PASSWORD IS NEVER ASKED FOR TWICE. The server drops it before the list
 * leaves the process, so what this page holds is an empty string plus
 * `hasPassword`; posting the empty string back is what tells the server to keep
 * what it has. That is also why the password box is not simply blank: a blank
 * box on a working proxy reads as data loss, and the user retypes the very
 * secret this arrangement exists to avoid moving around.
 *
 * NO ACCENT ANYWHERE. Nearly every row in a connection list is switched on, and
 * a column filled with gold would claim seven things are happening — the accent
 * means activity. Same ruling as the module list and Wave 1's Enabled column.
 * The one primary button on the page is Add.
 */

// The seven types the server accepts, in the order /api/options lists them. They
// are protocol identifiers rather than words, so only the two that are English
// are translated.
const KINDS = ['none', 'direct', 'http', 'https', 'socks4', 'socks4a', 'socks5'] as const;
type Kind = (typeof KINDS)[number];

/** One row, as the server sends it and as it is posted back. */
interface Connection {
  id: string;
  type: Kind;
  host?: string;
  port?: number;
  username?: string;
  password?: string;
  /**
   * Whether the SERVER holds a password for this row. Derived there and never
   * stored; the page may read it and must never invent it.
   */
  hasPassword?: boolean;
  enabled: boolean;
  order: number;
  filter?: string[];
  maxDownloads?: number;
}

/** How far a probe got. Mirrors proxycfg.Report. */
interface Report {
  ok: boolean;
  stage: 'refused' | 'dial' | 'auth' | 'connect';
  detail: string;
  millis: number;
}

interface Rejection {
  line: number;
  reason: string;
}

interface ImportResult {
  entries: Connection[];
  rejected: Rejection[];
}

/**
 * The strings this page needs, keyed by where they are going.
 *
 * Same arrangement as tx.ts, and for the same reason: the locale files are one
 * writer's lane per wave, and English literals scattered through a component are
 * a hunt when the translation wave arrives. The lookup asks the real catalogue
 * first, so the day these keys land in en.ts this table stops being consulted
 * and can be deleted without touching anything else here.
 */
const PENDING = {
  'settings.connections.add': 'Add connection',
  'settings.connections.import': 'Import list',
  'settings.connections.listTitle': 'Outbound connections',
  'settings.connections.empty': 'Everything goes out over this machine',
  'settings.connections.emptyHint':
    'No outbound connections are configured, so every download uses this machine’s own connection. Add a proxy to route downloads through, or a direct row to keep certain hosts off one.',
  'settings.connections.use': 'Use this connection',
  'settings.connections.moveUp': 'Move up',
  'settings.connections.moveDown': 'Move down',
  'settings.connections.remove': 'Remove this connection',
  'settings.connections.edit': 'Edit this connection',
  'settings.connections.type': 'Type',
  'settings.connections.typeHint':
    'None and direct are not the same row. None is inert: it names no connection and is never used, so it survives only until you finish filling it in. Direct is a real choice — go out over this machine’s own connection and deliberately bypass every proxy for the hosts named below, which is how a NAS is excluded from a whole-app proxy. A row whose filter matches the host beats a row with no filter, so a direct row with a filter always wins over a catch-all proxy.',
  'settings.connections.kind.none': 'None',
  'settings.connections.kind.direct': 'Direct',
  'settings.connections.stateNone': 'Inert. Nothing is ever sent through this row.',
  'settings.connections.stateDirect': 'Bypasses every proxy for the hosts below.',
  'settings.connections.warnDirectCatchAll':
    'This direct row has no host filter, so it takes its turn in the rotation and sends downloads out unproxied at random. Name the hosts it should claim.',
  'settings.connections.stateSocks4':
    'SOCKS4 carries a user id and has no password field at all, so no password is stored for this row.',
  'settings.connections.host': 'Host',
  'settings.connections.port': 'Port',
  'settings.connections.username': 'User name',
  'settings.connections.usernameHint':
    'The proxy’s own credentials, not a hoster account. Clearing the user name clears the stored password with it.',
  'settings.connections.password': 'Password',
  'settings.connections.passwordStored': 'stored — leave empty to keep it',
  'settings.connections.passwordHint':
    'A stored password is never sent to this page, which is why the box is empty. Leave it empty and the saved one is kept. A stored password does not follow the row to a different host, port or type: change one of those and this has to be set again.',
  'settings.connections.filter': 'Host filter',
  'settings.connections.filterHint':
    'One host per line. A bare domain covers everything under it, so example.org is enough for dl2.example.org, and * ? [ ] work as wildcards. Empty means this row is a catch-all, which is weaker than a row whose filter matches the host.',
  'settings.connections.filterAll': 'all hosts',
  'settings.connections.filterCount': '{n} hosts',
  'settings.connections.cap': 'Downloads at once',
  'settings.connections.capHint':
    'How many downloads may share this connection at the same time. 0 uses the default of 2 — spreading downloads is what the list is for, and one connection taking the whole queue would defeat it.',
  'settings.connections.capDefault': 'default',
  'settings.connections.test': 'Test',
  'settings.connections.testing': 'Testing…',
  'settings.connections.testTarget': 'Test against',
  'settings.connections.testTargetHint':
    'Optional. Left empty the test only shows that the proxy answers. Name a host and the proxy is asked to forward to it, which is what actually checks the credentials and shows whether that one hoster is being refused.',
  'settings.connections.testFailed': 'The test could not be run: {error}',
  'settings.connections.importTitle': 'Import a proxy list',
  'settings.connections.importLabel': 'Proxy list',
  'settings.connections.importHint':
    'One per line, as socks5://user:pass@host:port. https, socks4 and socks4a work too, and a line that cannot be read is listed below with the reason rather than dropped.',
  'settings.connections.importPlaceholder': 'socks5://user:pass@proxy.example.org:1080',
  'settings.connections.importRead': 'Read list',
  'settings.connections.importReading': 'Reading…',
  'settings.connections.importReady': '{n} ready to add',
  'settings.connections.importAdd': 'Add {n}',
  'settings.connections.importRefused': '{n} refused',
  'settings.connections.importNothing': 'Nothing in this list could be read.',
  'settings.connections.importLine': 'Line {n}',
  'settings.connections.importFailed': 'The list could not be read: {error}',
  'settings.connections.cancel': 'Cancel',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      // The cast is the whole point: these keys are not in the union yet. It is
      // narrow — only keys in PENDING can be passed — and it goes with the table.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

/**
 * The settings type in lib/api.ts does not name `connections` yet, and the draft
 * carries it regardless — see the note on SettingsDraft.cfg. These two casts are
 * the whole of that gap, and both disappear the moment the field is declared
 * there.
 */
function readConnections(cfg: unknown): Connection[] {
  return (cfg as { connections?: Connection[] }).connections ?? [];
}

/** A client-side id for a row that has never been saved.
 *
 *  Not left blank: the server fills a blank one in, but two new rows would both
 *  be blank until then, and React would key them identically — the second row's
 *  keystrokes would land in the first. Sanitize keeps any id that is unique, so
 *  this one simply becomes the stored id.
 */
let newRowCounter = 0;
const freshID = () => `n${Date.now().toString(36)}${newRowCounter++}`;

export function Connections() {
  const cx = useCx();
  const { cfg, patch } = useDraft();
  const rows = readConnections(cfg);

  const [openRow, setOpenRow] = useState<string>('');
  const [importing, setImporting] = useState(false);

  // Written back with the position renumbered from the array, because the order
  // field is what the server sorts on: leaving it stale after a move would show
  // one sequence here and walk another at download time.
  const write = useCallback(
    (next: Connection[]) => {
      const ordered = next.map((c, i) => ({ ...c, order: i }));
      patch({ connections: ordered } as unknown as Parameters<typeof patch>[0]);
    },
    [patch],
  );

  const update = (id: string, fields: Partial<Connection>) =>
    write(rows.map((c) => (c.id === id ? { ...c, ...fields } : c)));

  const move = (index: number, by: number) => {
    const to = index + by;
    if (to < 0 || to >= rows.length) return;
    const next = [...rows];
    [next[index], next[to]] = [next[to], next[index]];
    write(next);
  };

  const add = () => {
    const row: Connection = { id: freshID(), type: 'http', enabled: true, order: rows.length, port: 8080 };
    write([...rows, row]);
    setOpenRow(row.id);
  };

  return (
    <div className="flex flex-col gap-10">
      <Card className="flex flex-col gap-4">
        <SectionTitle
          hue={0}
          right={
            <div className="flex items-center gap-2">
              <Button kind="secondary" onClick={() => setImporting(true)}>
                {cx('settings.connections.import')}
              </Button>
              <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
                {cx('settings.connections.add')}
              </Button>
            </div>
          }
        >
          {cx('settings.connections.listTitle')}
        </SectionTitle>

        {rows.length === 0 ? (
          // Inside the card rather than instead of it: the add and import
          // buttons above are the way out of this state, and swapping the card
          // for an EmptyState would take them off the page.
          <p className="py-6 text-center text-sm text-carbon-textSub">
            {cx('settings.connections.empty')}
            <span className="mt-1 block text-[11px] text-carbon-textMuted">
              {cx('settings.connections.emptyHint')}
            </span>
          </p>
        ) : (
          <ul className="flex flex-col">
            {rows.map((row, i) => (
              <ConnectionRow
                key={row.id}
                row={row}
                index={i}
                last={i === rows.length - 1}
                open={openRow === row.id}
                onToggle={() => setOpenRow(openRow === row.id ? '' : row.id)}
                onChange={(fields) => update(row.id, fields)}
                onMove={(by) => move(i, by)}
                onRemove={() => write(rows.filter((c) => c.id !== row.id))}
              />
            ))}
          </ul>
        )}
      </Card>

      {importing && (
        <ImportDialog
          onClose={() => setImporting(false)}
          onAdd={(entries) => {
            // The parser has no ids to hand out — it does not know this draft —
            // so every imported row arrives with an empty one. Left as they came,
            // forty rows would share a React key, and editing or deleting any of
            // them would hit all forty.
            write([...rows, ...entries.map((e) => ({ ...e, id: freshID() }))]);
            setImporting(false);
          }}
        />
      )}
    </div>
  );
}

/** Hairline separators between rows; no boxes, and no vertical mark on the open
 *  one — the open row is distinguished by the well its editor sits in. */
function ConnectionRow({
  row,
  index,
  last,
  open,
  onToggle,
  onChange,
  onMove,
  onRemove,
}: {
  row: Connection;
  index: number;
  last: boolean;
  open: boolean;
  onToggle: () => void;
  onChange: (fields: Partial<Connection>) => void;
  onMove: (by: number) => void;
  onRemove: () => void;
}) {
  const cx = useCx();
  const inert = row.type === 'none' || row.type === 'direct';

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="group grid grid-cols-[auto_1fr_auto] items-center gap-3 py-2.5">
        <NeutralSwitch
          on={row.enabled}
          onChange={(v) => onChange({ enabled: v })}
          name={cx('settings.connections.use')}
          hue={index}
        />
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          aria-label={cx('settings.connections.edit')}
          className="flex min-w-0 items-center gap-3 text-left"
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="w-16 shrink-0 text-[11px] font-medium uppercase tracking-wide text-carbon-textSub">
            {kindLabel(cx, row.type)}
          </span>
          {/* dir=ltr: a host:port is never read right to left, whatever the
              interface language is. */}
          <span dir="ltr" className="min-w-0 flex-1 truncate text-sm text-carbon-text">
            {endpointOf(row) || <span className="text-carbon-textMuted">—</span>}
          </span>
          <span dir="ltr" className="hidden min-w-0 truncate text-xs text-carbon-textMuted sm:block sm:max-w-[14rem]">
            {filterSummary(cx, row.filter)}
          </span>
          <span className="glim-num hidden w-16 shrink-0 text-end text-xs text-carbon-textMuted md:block">
            {row.maxDownloads ? row.maxDownloads : cx('settings.connections.capDefault')}
          </span>
        </button>
        {/* Secondary actions on hover, and on keyboard focus, so a long list
            reads as content rather than as a wall of buttons. */}
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            icon={<IconArrowUp width={14} height={14} />}
            hue={index}
            title={cx('settings.connections.moveUp')}
            aria-label={cx('settings.connections.moveUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            icon={<IconArrowDown width={14} height={14} />}
            hue={index}
            title={cx('settings.connections.moveDown')}
            aria-label={cx('settings.connections.moveDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            kind="danger"
            icon={<IconTrash width={14} height={14} />}
            hue={index}
            title={cx('settings.connections.remove')}
            aria-label={cx('settings.connections.remove')}
            onClick={onRemove}
          />
        </div>
      </div>

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          <Editor row={row} onChange={onChange} />
          {!inert && <TestPanel row={row} />}
        </div>
      )}
    </li>
  );
}

function Editor({ row, onChange }: { row: Connection; onChange: (fields: Partial<Connection>) => void }) {
  const cx = useCx();
  const inert = row.type === 'none' || row.type === 'direct';
  const socks4 = row.type === 'socks4' || row.type === 'socks4a';

  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[10rem_1fr_6rem]">
        <Field label={cx('settings.connections.type')} hint={cx('settings.connections.typeHint')}>
          <Select value={row.type} onChange={(v) => onChange({ type: v as Kind })}>
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {kindLabel(cx, k)}
              </option>
            ))}
          </Select>
        </Field>
        {!inert && (
          <>
            <Field label={cx('settings.connections.host')}>
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={row.host ?? ''}
                placeholder="proxy.example.org"
                onChange={(e) => onChange({ host: e.target.value })}
              />
            </Field>
            <Field label={cx('settings.connections.port')}>
              <NumberInput
                value={row.port ?? 0}
                min={1}
                max={65535}
                onValue={(v) => onChange({ port: v })}
              />
            </Field>
          </>
        )}
      </div>

      {/* The state of this row, not an explanation of the feature — the
          explanation is behind the (i) on Type. Only the two kinds whose whole
          meaning is what they do NOT do carry a line. */}
      {row.type === 'none' && <StateLine tone="muted">{cx('settings.connections.stateNone')}</StateLine>}
      {row.type === 'direct' && <StateLine tone="muted">{cx('settings.connections.stateDirect')}</StateLine>}
      {row.type === 'direct' && (row.filter ?? []).length === 0 && (
        <StateLine tone="warn">{cx('settings.connections.warnDirectCatchAll')}</StateLine>
      )}

      {!inert && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={cx('settings.connections.username')} hint={cx('settings.connections.usernameHint')}>
            <TextInput
              dir="ltr"
              autoComplete="off"
              spellCheck={false}
              value={row.username ?? ''}
              onChange={(e) => onChange({ username: e.target.value })}
            />
          </Field>
          {!socks4 && (
            <Field label={cx('settings.connections.password')} hint={cx('settings.connections.passwordHint')}>
              <TextInput
                type="password"
                dir="ltr"
                autoComplete="new-password"
                value={row.password ?? ''}
                // The placeholder is what tells a stored password apart from no
                // password at all. Without it the box looks the same either way,
                // and a user looking at a working proxy concludes the password
                // was lost and types it in again.
                placeholder={row.hasPassword ? cx('settings.connections.passwordStored') : ''}
                onChange={(e) => onChange({ password: e.target.value })}
              />
            </Field>
          )}
        </div>
      )}
      {socks4 && <StateLine tone="muted">{cx('settings.connections.stateSocks4')}</StateLine>}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_10rem]">
        <Field label={cx('settings.connections.filter')} hint={cx('settings.connections.filterHint')}>
          <TextArea
            dir="ltr"
            rows={2}
            spellCheck={false}
            value={(row.filter ?? []).join('\n')}
            placeholder="example.org"
            // Split on save rather than per keystroke would lose a half-typed
            // line; splitting here keeps the field and the value the same thing.
            onChange={(e) =>
              onChange({ filter: e.target.value.split('\n').map((s) => s.trim()).filter(Boolean) })
            }
          />
        </Field>
        <Field label={cx('settings.connections.cap')} hint={cx('settings.connections.capHint')}>
          <NumberInput
            value={row.maxDownloads ?? 0}
            min={0}
            max={64}
            onValue={(v) => onChange({ maxDownloads: Math.max(0, v) })}
          />
        </Field>
      </div>
    </>
  );
}

/**
 * The Test button and what it found.
 *
 * The row is posted exactly as it is being edited, password and all — which for
 * a saved row is an empty password, and the server puts the stored one back
 * through the same merge a save goes through. So testing never asks anyone to
 * retype anything, and a test answers about precisely the connection a save
 * would write.
 */
function TestPanel({ row }: { row: Connection }) {
  const cx = useCx();
  const [target, setTarget] = useState('');
  const [busy, setBusy] = useState(false);
  const [report, setReport] = useState<Report | null>(null);

  async function run() {
    setBusy(true);
    setReport(null);
    try {
      const r = await fetch('/api/connections/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ entry: row, target }),
      });
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      setReport((await r.json()) as Report);
    } catch (e) {
      // A transport failure is not a verdict about the proxy, so it is reported
      // as a refusal to run rather than as the proxy being unreachable.
      setReport({
        ok: false,
        stage: 'refused',
        detail: cx('settings.connections.testFailed', { error: String(e).replace(/^Error:\s*/, '') }),
        millis: 0,
      });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-3 border-t border-carbon-border/60 pt-4">
      <div className="flex items-end gap-3">
        <div className="min-w-0 flex-1">
          <Field label={cx('settings.connections.testTarget')} hint={cx('settings.connections.testTargetHint')}>
            <TextInput
              dir="ltr"
              spellCheck={false}
              value={target}
              placeholder="example.org"
              onChange={(e) => setTarget(e.target.value)}
            />
          </Field>
        </div>
        <Button kind="secondary" onClick={run} disabled={busy} icon={<IconGlobe width={16} height={16} />}>
          {busy ? cx('settings.connections.testing') : cx('settings.connections.test')}
        </Button>
      </div>
      {report && (
        <p className={`text-xs ${report.ok ? 'text-statusOk' : 'text-statusFail'}`}>
          {report.detail}
          {report.ok && report.millis > 0 && <span className="glim-num text-carbon-textMuted"> · {report.millis} ms</span>}
        </p>
      )}
    </div>
  );
}

function ImportDialog({ onClose, onAdd }: { onClose: () => void; onAdd: (entries: Connection[]) => void }) {
  const cx = useCx();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);
  const [error, setError] = useState('');

  // The server reports a refused line by NUMBER and never sends the line back —
  // a rejected line is exactly where a password is still in plain text, and that
  // answer ends up in logs and screenshots. The text is right here, so the line
  // is put back together on this side.
  const lines = text.replace(/\r\n/g, '\n').split('\n');

  async function read() {
    setBusy(true);
    setError('');
    try {
      const r = await fetch('/api/connections/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      });
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      setResult((await r.json()) as ImportResult);
    } catch (e) {
      setError(cx('settings.connections.importFailed', { error: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setBusy(false);
    }
  }

  const ready = result?.entries.length ?? 0;

  return (
    <Modal
      title={cx('settings.connections.importTitle')}
      onClose={onClose}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" onClick={onClose}>
            {cx('settings.connections.cancel')}
          </Button>
          {result ? (
            <Button disabled={ready === 0} onClick={() => onAdd(result.entries)}>
              {cx('settings.connections.importAdd', { n: ready })}
            </Button>
          ) : (
            <Button disabled={busy || text.trim() === ''} onClick={read}>
              {busy ? cx('settings.connections.importReading') : cx('settings.connections.importRead')}
            </Button>
          )}
        </>
      }
    >
      <Field label={cx('settings.connections.importLabel')} hint={cx('settings.connections.importHint')}>
        <TextArea
          dir="ltr"
          rows={7}
          spellCheck={false}
          value={text}
          placeholder={cx('settings.connections.importPlaceholder')}
          // Any edit invalidates the last reading, so the Add button can never
          // add rows parsed from text that is no longer on screen.
          onChange={(e) => {
            setText(e.target.value);
            setResult(null);
          }}
        />
      </Field>

      {error && <p className="text-xs text-statusFail">{error}</p>}

      {result && (
        <div className="flex flex-col gap-2">
          <p className="text-xs text-carbon-textSub">
            {cx('settings.connections.importReady', { n: ready })}
            {result.rejected.length > 0 && (
              <span className="text-statusWarn">
                {' · '}
                {cx('settings.connections.importRefused', { n: result.rejected.length })}
              </span>
            )}
          </p>
          {ready === 0 && result.rejected.length === 0 && (
            <p className="text-xs text-carbon-textMuted">{cx('settings.connections.importNothing')}</p>
          )}
          {result.rejected.length > 0 && (
            // Every refusal, with the line it belongs to. This is the whole
            // reason the parser names them instead of dropping them: a list that
            // silently loses nine of forty is unattributable.
            <ul className="glim-well flex max-h-56 flex-col gap-2 overflow-y-auto p-3">
              {result.rejected.map((r) => (
                <li key={r.line} className="text-xs">
                  <span className="glim-num text-carbon-textMuted">
                    {cx('settings.connections.importLine', { n: r.line })}
                  </span>
                  {/* An explicit space, not only the margin: the margin is
                      visual, and without this a screen reader reads the number
                      and the line as one word. */}
                  {' '}
                  <span dir="ltr" className="ms-2 break-all text-carbon-textSub">
                    {lines[r.line - 1]}
                  </span>
                  <span className="mt-0.5 block text-statusWarn">{r.reason}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Modal>
  );
}

/** A fact about the row being edited, in one of the state hues. Not an
 *  explanation: those live behind the (i) beside the label. */
function StateLine({ tone, children }: { tone: 'muted' | 'warn'; children: ReactNode }) {
  return (
    <p className={`text-xs ${tone === 'warn' ? 'text-statusWarn' : 'text-carbon-textMuted'}`}>{children}</p>
  );
}

/** The one control the design language has no primitive for. Styled to match
 *  TextInput exactly, so a form row does not read as two different systems. */
function Select({
  value,
  onChange,
  children,
}: {
  value: string;
  onChange: (v: string) => void;
  children: ReactNode;
}) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text
        outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
    >
      {children}
    </select>
  );
}

function kindLabel(cx: (k: PendingKey) => string, kind: Kind): string {
  if (kind === 'none') return cx('settings.connections.kind.none');
  if (kind === 'direct') return cx('settings.connections.kind.direct');
  return kind; // a protocol identifier, not a word to translate
}

function endpointOf(row: Connection): string {
  if (row.type === 'none' || row.type === 'direct') return '';
  if (!row.host) return '';
  // Bracketed, because an IPv6 literal read against a port is a different
  // machine entirely — the same reason the Go side joins it this way.
  const host = row.host.includes(':') ? `[${row.host}]` : row.host;
  return row.port ? `${host}:${row.port}` : host;
}

function filterSummary(cx: (k: PendingKey, vars?: Record<string, string | number>) => string, filter?: string[]): string {
  const list = filter ?? [];
  if (list.length === 0) return cx('settings.connections.filterAll');
  if (list.length === 1) return list[0];
  return cx('settings.connections.filterCount', { n: list.length });
}
