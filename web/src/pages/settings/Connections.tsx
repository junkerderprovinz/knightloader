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
import { IconArrowDown, IconArrowUp, IconClose, IconGlobe, IconPlus, IconTrash } from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useDraft } from './context';
import { NeutralSwitch } from './controls';
import { ModuleToggle } from './ModuleToggle';

/**
 * Connections manages the ordered list of outbound connections downloads are
 * spread across. The list lives in the settings draft, because PUT
 * /api/settings validates the rows and merges back the passwords the client
 * never sees; an empty password with `hasPassword` means "keep the stored one".
 * No row wears the accent, since nearly every row is switched on.
 */

// The types the server accepts, in /api/options order. They are protocol
// identifiers, so only the two English words are translated.
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
  /** Whether the server holds a password for this row; derived there, never set here. */
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
 * PENDING holds the English strings until the catalogue has them; the lookup
 * asks the catalogue first.
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
    'None and direct are not the same row. None is inert: it names no connection and is never used, so it survives only until you finish filling it in. Direct is a real choice - go out over this machine’s own connection and deliberately bypass every proxy for the hosts named below, which is how a NAS is excluded from a whole-app proxy. A row whose filter matches the host beats a row with no filter, so a direct row with a filter always wins over a catch-all proxy.',
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
  'settings.connections.passwordStored': 'stored - leave empty to keep it',
  'settings.connections.passwordHint':
    'A stored password is never sent to this page, which is why the box is empty. Leave it empty and the saved one is kept. A stored password does not follow the row to a different host, port or type: change one of those and this has to be set again.',
  'settings.connections.filter': 'Host filter',
  'settings.connections.filterHint':
    'One host per line. A bare domain covers everything under it, so example.org is enough for dl2.example.org, and * ? [ ] work as wildcards. Empty means this row is a catch-all, which is weaker than a row whose filter matches the host.',
  'settings.connections.filterAll': 'all hosts',
  'settings.connections.filterCount': '{n} hosts',
  'settings.connections.cap': 'Downloads at once',
  'settings.connections.capHint':
    'How many downloads may share this connection at the same time. 0 uses the default of 2 - spreading downloads is what the list is for, and one connection taking the whole queue would defeat it.',
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
      // These keys are not in the union yet; only PENDING keys can be passed.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

/** lib/api.ts's Settings does not declare `connections`, hence the casts. */
function readConnections(cfg: unknown): Connection[] {
  return (cfg as { connections?: Connection[] }).connections ?? [];
}

// A client-side id for a new row, so two unsaved rows never share a React key.
// Sanitize keeps any unique id.
let newRowCounter = 0;
const freshID = () => `n${Date.now().toString(36)}${newRowCounter++}`;

export function Connections() {
  const cx = useCx();
  const { cfg, patch } = useDraft();
  const rows = readConnections(cfg);

  const [openRow, setOpenRow] = useState<string>('');
  const [importing, setImporting] = useState(false);

  // Positions are renumbered on every write, since the server sorts by them.
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
      <Card hue={0} className="flex flex-col gap-4">
        <SectionTitle
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
        <ModuleToggle id="connections" />

        {rows.length === 0 ? (
          // Inside the card rather than an EmptyState, which would hide Add.
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
            // The parser has no ids to hand out, so every imported row gets one.
            write([...rows, ...entries.map((e) => ({ ...e, id: freshID() }))]);
            setImporting(false);
          }}
        />
      )}
    </div>
  );
}

/** ConnectionRow draws hairlines between rows; the open row's editor sits in a well. */
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
          <span dir="ltr" className="min-w-0 flex-1 truncate text-sm text-carbon-text">
            {endpointOf(row) || <span className="text-carbon-textMuted">-</span>}
          </span>
          <span dir="ltr" className="hidden min-w-0 truncate text-xs text-carbon-textMuted sm:block sm:max-w-[14rem]">
            {filterSummary(cx, row.filter)}
          </span>
          <span className="glim-num hidden w-16 shrink-0 text-end text-xs text-carbon-textMuted md:block">
            {row.maxDownloads ? row.maxDownloads : cx('settings.connections.capDefault')}
          </span>
        </button>
        {/* Row actions show on hover and focus. `labelled` makes them follow the
            Beschriftung setting; the summary truncates instead. */}
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            labelled
            icon={<IconArrowUp width={16} height={16} />}
            hue={index}
            title={cx('settings.connections.moveUp')}
            aria-label={cx('settings.connections.moveUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            labelled
            icon={<IconArrowDown width={16} height={16} />}
            hue={index}
            title={cx('settings.connections.moveDown')}
            aria-label={cx('settings.connections.moveDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            labelled
            icon={<IconTrash width={16} height={16} />}
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

      {/* A state line only for the two kinds defined by what they do not do. */}
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
                // Tells a stored password apart from none.
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
            // Split per keystroke, so the field and the value stay the same text.
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
 * TestPanel posts the row exactly as edited; the server merges a stored
 * password back as a save would, so the test covers what a save would write.
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
      // A transport failure is reported as a refusal to run, not as a verdict.
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
  const { toast } = useToast();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);
  // Keyed onto the reading button so a repeated refusal shakes again.
  const [shake, setShake] = useState(0);

  // The server reports a refused line by number only, since the line may hold
  // a plain-text password; the text is rebuilt here.
  const lines = text.replace(/\r\n/g, '\n').split('\n');

  async function read() {
    setBusy(true);
    try {
      const r = await fetch('/api/connections/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      });
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      setResult((await r.json()) as ImportResult);
    } catch (e) {
      toast(cx('settings.connections.importFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setShake((n) => n + 1);
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
          <Button kind="ghost" labelled icon={<IconClose />} title={cx('settings.connections.cancel')} onClick={onClose} />
          {/* The spacer puts the advancing button at the end of the row. */}
          {result ? (
            <Button disabled={ready === 0} onClick={() => onAdd(result.entries)}>
              {cx('settings.connections.importAdd', { n: ready })}
            </Button>
          ) : (
            <Button
              key={shake}
              className={shake > 0 ? 'glim-shake' : ''}
              disabled={busy || text.trim() === ''}
              onClick={read}
            >
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
          // Any edit invalidates the last reading, so Add never uses stale rows.
          onChange={(e) => {
            setText(e.target.value);
            setResult(null);
          }}
        />
      </Field>

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
            // Every refused line, named, so nothing is lost silently.
            <ul className="glim-well flex max-h-56 flex-col gap-2 overflow-y-auto p-3">
              {result.rejected.map((r) => (
                <li key={r.line} className="text-xs">
                  <span className="glim-num text-carbon-textMuted">
                    {cx('settings.connections.importLine', { n: r.line })}
                  </span>
                  {/* A real space, so a screen reader does not run number and line together. */}
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

/** StateLine is a fact about the row in a state hue; explanations sit behind the (i). */
function StateLine({ tone, children }: { tone: 'muted' | 'warn'; children: ReactNode }) {
  return (
    <p className={`text-xs ${tone === 'warn' ? 'text-statusWarn' : 'text-carbon-textMuted'}`}>{children}</p>
  );
}

/**
 * wheelSteps lets a closed <select> step one option per wheel notch, clamped
 * at both ends, and fires a real `change`. It attaches a non-passive listener
 * because React's onWheel is passive. The same listener lives in SearchField,
 * QueueBar and RuleEditor.
 */
function wheelSteps(el: HTMLSelectElement | null) {
  if (!el) return;
  const onWheel = (e: WheelEvent) => {
    // Only the sign of deltaY counts; trackpads report fractions.
    if (el.disabled || el.options.length < 2 || e.deltaY === 0) return;
    e.preventDefault();
    const next = Math.min(el.options.length - 1, Math.max(0, el.selectedIndex + (e.deltaY > 0 ? 1 : -1)));
    if (next === el.selectedIndex) return;
    el.selectedIndex = next;
    // A real change event, so the element's onChange handles it like a click.
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  el.addEventListener('wheel', onWheel, { passive: false });
  return () => el.removeEventListener('wheel', onWheel);
}

/** Select is styled to match TextInput, since the design language has no select. */
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
      ref={wheelSteps}
      onChange={(e) => onChange(e.target.value)}
      className="glim-select appearance-none pe-6 w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text
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
  // Bracketed, as the Go side does, so an IPv6 literal is not read with the port.
  const host = row.host.includes(':') ? `[${row.host}]` : row.host;
  return row.port ? `${host}:${row.port}` : host;
}

function filterSummary(cx: (k: PendingKey, vars?: Record<string, string | number>) => string, filter?: string[]): string {
  const list = filter ?? [];
  if (list.length === 0) return cx('settings.connections.filterAll');
  if (list.length === 1) return list[0];
  return cx('settings.connections.filterCount', { n: list.length });
}
