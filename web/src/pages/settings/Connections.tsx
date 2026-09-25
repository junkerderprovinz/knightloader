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
import { Dropdown } from '../../components/Dropdown';
import { IconArrowDown, IconArrowUp, IconClose, IconGlobe, IconPlus, IconTrash } from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { fmtUnit } from '../../lib/format';
import { useT } from '../../lib/i18n';
import { useDraft } from './context';
import { NeutralSwitch, RowRefusal } from './controls';
import { ModuleToggle } from './ModuleToggle';

/**
 * ConnectionsCard manages the ordered list of outbound connections downloads
 * are spread across. The list lives in the settings draft, because PUT
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

type Translate = ReturnType<typeof useT>['t'];

/** lib/api.ts's Settings does not declare `connections`, hence the casts. */
function readConnections(cfg: unknown): Connection[] {
  return (cfg as { connections?: Connection[] }).connections ?? [];
}

// A client-side id for a new row, so two unsaved rows never share a React key.
// Sanitize keeps any unique id.
let newRowCounter = 0;
const freshID = () => `n${Date.now().toString(36)}${newRowCounter++}`;

export function ConnectionsCard({ hue }: { hue: number }) {
  const { t } = useT();
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
    <>
      <Card hue={hue} className="flex flex-col gap-4">
        <SectionTitle
          right={
            <div className="flex items-center gap-2">
              <Button kind="secondary" onClick={() => setImporting(true)}>
                {t('settings.connections.import')}
              </Button>
              <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
                {t('settings.connections.add')}
              </Button>
            </div>
          }
        >
          {t('settings.module.connections')}
        </SectionTitle>
        <ModuleToggle id="connections" />

        {rows.length === 0 ? (
          // Inside the card rather than an EmptyState, which would hide Add.
          <p className="py-6 text-center text-sm text-carbon-textSub">
            {t('settings.connections.empty')}
            <span className="mt-1 block text-[11px] text-carbon-textMuted">
              {t('settings.connections.emptyHint')}
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
    </>
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
  const { t } = useT();
  const inert = row.type === 'none' || row.type === 'direct';

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="grid grid-cols-[auto_1fr_auto] items-center gap-3 py-2.5">
        <NeutralSwitch
          on={row.enabled}
          onChange={(v) => onChange({ enabled: v })}
          name={t('settings.connections.use')}
          hue={index}
        />
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          aria-label={t('settings.connections.edit')}
          className="flex min-w-0 items-center gap-3 text-start"
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="w-16 shrink-0 text-[11px] font-medium uppercase tracking-wide text-carbon-textSub">
            {kindLabel(t, row.type)}
          </span>
          <span dir="ltr" className="min-w-0 flex-1 truncate text-sm text-carbon-text">
            {endpointOf(row) || <span className="text-carbon-textMuted">-</span>}
          </span>
          <span dir="ltr" className="hidden min-w-0 truncate text-xs text-carbon-textMuted sm:block sm:max-w-[14rem]">
            {filterSummary(t, row.filter)}
          </span>
          <span className="glim-num hidden w-16 shrink-0 text-end text-xs text-carbon-textMuted md:block">
            {row.maxDownloads ? row.maxDownloads : t('settings.connections.capDefault')}
          </span>
        </button>
        {/* `labelled`, so the actions follow the Beschriftung setting; the summary
            truncates instead. */}
        <div className="flex items-center gap-1.5">
          <IconBadge
            labelled
            icon={<IconArrowUp width={16} height={16} />}
            hue={index}
            title={t('settings.connections.moveUp')}
            aria-label={t('settings.connections.moveUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            labelled
            icon={<IconArrowDown width={16} height={16} />}
            hue={index}
            title={t('settings.connections.moveDown')}
            aria-label={t('settings.connections.moveDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            labelled
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={t('settings.connections.remove')}
            aria-label={t('settings.connections.remove')}
            onClick={onRemove}
          />
        </div>
      </div>

      <RowRefusal field={`connections.${index}`} className="ps-12" />

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
  const { t } = useT();
  const inert = row.type === 'none' || row.type === 'direct';
  const socks4 = row.type === 'socks4' || row.type === 'socks4a';

  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[10rem_1fr_6rem]">
        <Field label={t('settings.connections.type')} hint={t('settings.connections.typeHint')}>
          <Dropdown
            label={t('settings.connections.type')}
            value={row.type}
            onChange={(v) => onChange({ type: v })}
            options={KINDS.map((k) => ({ value: k, label: kindLabel(t, k) }))}
          />
        </Field>
        {!inert && (
          <>
            <Field label={t('settings.connections.host')}>
              <TextInput
                dir="ltr"
                spellCheck={false}
                value={row.host ?? ''}
                placeholder="proxy.example.org"
                onChange={(e) => onChange({ host: e.target.value })}
              />
            </Field>
            <Field label={t('settings.connections.port')}>
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

      {row.type === 'direct' && (row.filter ?? []).length === 0 && (
        <StateLine>{t('settings.connections.warnDirectCatchAll')}</StateLine>
      )}

      {!inert && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field
            label={t('settings.connections.username')}
            hint={
              socks4
                ? [t('settings.connections.stateSocks4'), t('settings.connections.usernameHint')]
                : t('settings.connections.usernameHint')
            }
          >
            <TextInput
              dir="ltr"
              autoComplete="off"
              spellCheck={false}
              value={row.username ?? ''}
              onChange={(e) => onChange({ username: e.target.value })}
            />
          </Field>
          {!socks4 && (
            <Field label={t('settings.connections.password')} hint={t('settings.connections.passwordHint')}>
              <TextInput
                type="password"
                dir="ltr"
                autoComplete="new-password"
                value={row.password ?? ''}
                // Tells a stored password apart from none.
                placeholder={row.hasPassword ? t('settings.connections.passwordStored') : ''}
                onChange={(e) => onChange({ password: e.target.value })}
              />
            </Field>
          )}
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_10rem]">
        <Field label={t('settings.connections.filter')} hint={t('settings.connections.filterHint')}>
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
        <Field label={t('settings.connections.cap')} hint={t('settings.connections.capHint')}>
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
  const { t } = useT();
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
        detail: t('settings.connections.testFailed', { error: String(e).replace(/^Error:\s*/, '') }),
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
          <Field label={t('settings.connections.testTarget')} hint={t('settings.connections.testTargetHint')}>
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
          {busy ? t('settings.connections.testing') : t('settings.connections.test')}
        </Button>
      </div>
      {report && (
        <p className={`text-xs ${report.ok ? 'text-statusOk' : 'text-statusFail'}`}>
          {report.detail}
          {report.ok && report.millis > 0 && <span className="glim-num text-carbon-textMuted"> · {fmtUnit(report.millis, 'ms')}</span>}
        </p>
      )}
    </div>
  );
}

function ImportDialog({ onClose, onAdd }: { onClose: () => void; onAdd: (entries: Connection[]) => void }) {
  const { t } = useT();
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
      toast(t('settings.connections.importFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  const ready = result?.entries.length ?? 0;

  return (
    <Modal
      title={t('settings.connections.importTitle')}
      onClose={onClose}
      footer={
        <>
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('settings.connections.cancel')} onClick={onClose} />
          {/* The spacer puts the advancing button at the end of the row. */}
          {result ? (
            <Button disabled={ready === 0} onClick={() => onAdd(result.entries)}>
              {t('settings.connections.importAdd', { n: ready })}
            </Button>
          ) : (
            <Button
              key={shake}
              className={shake > 0 ? 'glim-shake' : ''}
              disabled={busy || text.trim() === ''}
              onClick={read}
            >
              {busy ? t('settings.connections.importReading') : t('settings.connections.importRead')}
            </Button>
          )}
        </>
      }
    >
      <Field label={t('settings.connections.importLabel')} hint={t('settings.connections.importHint')}>
        <TextArea
          dir="ltr"
          rows={7}
          spellCheck={false}
          value={text}
          placeholder={t('settings.connections.importPlaceholder')}
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
            {t('settings.connections.importReady', { n: ready })}
            {result.rejected.length > 0 && (
              <span className="text-statusWarn">
                {' · '}
                {t('settings.connections.importRefused', { n: result.rejected.length })}
              </span>
            )}
          </p>
          {ready === 0 && result.rejected.length === 0 && (
            <p className="text-xs text-carbon-textMuted">{t('settings.connections.importNothing')}</p>
          )}
          {result.rejected.length > 0 && (
            // Every refused line, named, so nothing is lost silently.
            <ul className="glim-well flex max-h-56 flex-col gap-2 overflow-y-auto p-3">
              {result.rejected.map((r) => (
                <li key={r.line} className="text-xs">
                  <span className="glim-num text-carbon-textMuted">
                    {t('settings.connections.importLine', { n: r.line })}
                  </span>
                  {/* A real space, so a screen reader does not run number and line together. */}
                  {' '}
                  <span dir="ltr" className="ms-2 break-all text-carbon-textSub">
                    {lines[r.line - 1]}
                  </span>
                  <span dir="auto" className="mt-0.5 block text-statusWarn">{r.reason}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Modal>
  );
}

/** StateLine is a warning about the row; explanations sit behind the (i). */
function StateLine({ children }: { children: ReactNode }) {
  return <p className="text-xs text-statusWarn">{children}</p>;
}

function kindLabel(t: Translate, kind: Kind): string {
  if (kind === 'none') return t('settings.connections.kind.none');
  if (kind === 'direct') return t('settings.connections.kind.direct');
  return kind; // a protocol identifier, not a word to translate
}

function endpointOf(row: Connection): string {
  if (row.type === 'none' || row.type === 'direct') return '';
  if (!row.host) return '';
  // Bracketed, as the Go side does, so an IPv6 literal is not read with the port.
  const host = row.host.includes(':') ? `[${row.host}]` : row.host;
  return row.port ? `${host}:${row.port}` : host;
}

function filterSummary(t: Translate, filter?: string[]): string {
  const list = filter ?? [];
  if (list.length === 0) return t('settings.connections.filterAll');
  if (list.length === 1) return list[0];
  return t('settings.connections.filterCount', { n: list.length });
}
