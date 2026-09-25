import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  IconBadge,
  InfoBubble,
  NumberInput,
  SectionTitle,
  TextArea,
  TextInput,
} from '../../components/ui';
import { Dropdown } from '../../components/Dropdown';
import { Tabs } from '../../components/Tabs';
import {
  IconArrowDown,
  IconArrowUp,
  IconCollector,
  IconGlobe,
  IconInstances,
  IconPlay,
  IconPlus,
  IconRetry,
  IconSearch,
  IconTrash,
} from '../../lib/icons';
import { useT } from '../../lib/i18n';
import { useDraft } from './context';
import { ModuleToggle } from './ModuleToggle';

/**
 * ReconnectCards set how this box asks the router for a new public address.
 * Only the chosen method's fields are shown; hidden values stay in the draft.
 * The stored router password comes back masked, the box stays empty and the
 * draft keeps the mask, which the save merges back. The check URL has no
 * default, so no address is reported to a service nobody chose. Automatic
 * reconnects run in internal/app/app_dispatch.go; these cards only run one on
 * request.
 */

/** Mirrors reconnect.Config. */
type Method = 'none' | 'command' | 'http' | 'upnp' | 'script';

interface ReconnectRequest {
  method?: string;
  url: string;
  headers?: Record<string, string>;
  body?: string;
}

interface ReconnectConfig {
  method: Method;
  username?: string;
  password?: string;
  router?: string;
  command?: string;
  args?: string[];
  requests?: ReconnectRequest[];
  interpreter?: string;
  interpreterArgs?: string[];
  script?: string;
  upnpLocation?: string;
  checkUrl?: string;
  intervalSeconds: number;
  timeoutSeconds: number;
}

/** GET /api/reconnect. */
export interface ReconnectState {
  busy: boolean;
  configured: boolean;
  /** The server's English sentence, used when the code has no translation. */
  reason?: string;
  /** The same fact as a code, which can be translated. */
  reasonCode?: string;
  reasonN?: number;
  reasonMethod?: string;
  reasonVar?: string;
}

/**
 * useReasonText translates the reason code and falls back to the server's
 * English sentence for a code this build does not know.
 */
export function useReasonText(state: ReconnectState | null): string {
  const { t } = useT();
  if (!state) return '';
  const code = state.reasonCode;
  if (!code) return state.reason ?? '';
  const key = `settings.reconnect.reason.${code}` as never;
  const text = t(key, { n: state.reasonN ?? 0, method: state.reasonMethod ?? '', var: state.reasonVar ?? '' });
  // t() returns the key itself when there is no entry.
  return text === key ? (state.reason ?? '') : text;
}

export async function fetchReconnectState(): Promise<ReconnectState> {
  const r = await fetch('/api/reconnect');
  if (!r.ok) throw new Error(String(r.status));
  return (await r.json()) as ReconnectState;
}

/** POST /api/reconnect. */
export interface RunResult {
  oldIp: string;
  newIp: string;
  checks: number;
  tookMs: number;
}

/**
 * runReconnect runs one reconnect with the saved settings. Null means another
 * run already holds the router, which is not a failure: its result belongs to
 * whoever started it.
 */
export async function runReconnect(): Promise<RunResult | null> {
  const r = await fetch('/api/reconnect', { method: 'POST' });
  if (r.status === 409) return null;
  if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
  return (await r.json()) as RunResult;
}

/** One line the import could not map. Mirrors reconnect.Problem. */
interface Problem {
  line: number;
  text: string;
  why: string;
}

/** POST /api/reconnect/import. */
interface ImportResult {
  requests: ReconnectRequest[];
  problems?: Problem[];
  variables?: string[];
  error?: string;
}

/** GET /api/reconnect/router. */
interface RouterAddress {
  address: string;
  interface?: string;
}

/** The mask for a stored router password; a protocol value shared with reconnect.RedactedPassword. */
const REDACTED = '********';

/**
 * The band reconnect.Sanitize folds these numbers into and the value it uses
 * when one is unset, shown so a save does not change a number unexpectedly.
 */
const INTERVAL = { lo: 1, hi: 60, fallback: 5 };
const TIMEOUT = { lo: 5, hi: 900, fallback: 120 };

const DEFAULTS: ReconnectConfig = {
  method: 'none',
  intervalSeconds: INTERVAL.fallback,
  timeoutSeconds: TIMEOUT.fallback,
};

/**
 * The methods in the order the Go constants declare them. "none" is not among
 * them: off is the module switch above, which keeps the method for later.
 */
const METHODS: { id: Exclude<Method, 'none'>; icon: ReactNode }[] = [
  { id: 'command', icon: <IconPlay width={16} height={16} /> },
  { id: 'http', icon: <IconGlobe width={16} height={16} /> },
  { id: 'upnp', icon: <IconInstances width={16} height={16} /> },
  { id: 'script', icon: <IconCollector width={16} height={16} /> },
];

/**
 * Check services offered as a shortcut, all plain-text endpoints. Host names
 * are not translated.
 */
const CHECK_PRESETS: { id: string; url: string }[] = [
  { id: 'ipify', url: 'https://api.ipify.org' },
  { id: 'icanhazip', url: 'https://icanhazip.com' },
  { id: 'ifconfig.me', url: 'https://ifconfig.me/ip' },
  { id: 'ipinfo.io', url: 'https://ipinfo.io/ip' },
  { id: 'checkip.amazonaws.com', url: 'https://checkip.amazonaws.com' },
];

/** lib/api.ts's Settings does not declare `reconnect`, hence the casts. */
function readReconnect(cfg: unknown): ReconnectConfig {
  return { ...DEFAULTS, ...((cfg as { reconnect?: ReconnectConfig }).reconnect ?? {}) };
}

/** The setup card takes `hue`, the check and run cards the two after it. */
export function ReconnectCards({ hue }: { hue: number }) {
  const { t } = useT();
  const { cfg, patch } = useDraft();
  const rc = readReconnect(cfg);

  const write = useCallback(
    (fields: Partial<ReconnectConfig>) => {
      const next = { ...readReconnect(cfg), ...fields };
      patch({ reconnect: next } as unknown as Parameters<typeof patch>[0]);
    },
    [cfg, patch],
  );

  const [state, setState] = useState<ReconnectState | null>(null);

  // Polled, since a hoster limit can start a run while the page is open.
  useEffect(() => {
    let alive = true;
    const read = async () => {
      try {
        const s = await fetchReconnectState();
        if (alive) setState(s);
      } catch {
        if (alive) setState(null);
      }
    };
    void read();
    const timer = window.setInterval(read, 5000);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, []);

  const off = rc.method === 'none';

  return (
    <>
      <Card hue={hue} className="flex flex-col gap-5">
        <SectionTitle>{t('settings.module.reconnect')}</SectionTitle>
        <ModuleToggle id="reconnect" />
        {/* FieldGroup, because a Field's label would pass a click on the
            caption to the first tab. */}
        <FieldGroup layout="row" label={t('settings.reconnect.method')} hint={t('settings.reconnect.methodHint')}>
          <Tabs
            label={t('settings.reconnect.method')}
            variant="well"
            size="sm"
            active={off ? null : rc.method}
            onSelect={(id) => write({ method: id as Method })}
            items={METHODS.map((m) => ({
              id: m.id,
              // The ids are a literal union, so each template is a real key.
              label: t(`settings.reconnect.method.${m.id}`),
              icon: m.icon,
            }))}
          />
        </FieldGroup>

        {off && <StateLine tone="muted">{t('settings.reconnect.offState')}</StateLine>}
        {rc.method === 'command' && <CommandFields rc={rc} write={write} />}
        {rc.method === 'http' && <RequestFields rc={rc} write={write} />}
        {rc.method === 'upnp' && <UPnPFields rc={rc} write={write} />}
        {rc.method === 'script' && <ScriptFields rc={rc} write={write} />}
        {usesRouterFields(rc.method) && <RouterFields rc={rc} write={write} />}
      </Card>

      {!off && (
        <Card hue={hue + 1} className="flex flex-col gap-5">
          <SectionTitle>{t('settings.reconnect.checkTitle')}</SectionTitle>
          <CheckFields rc={rc} write={write} />
        </Card>
      )}

      <Card hue={hue + 2} className="flex flex-col gap-4">
        <SectionTitle>{t('settings.reconnect.runTitle')}</SectionTitle>
        <RunPanel state={state} disabled={off} />
      </Card>
    </>
  );
}

/** usesRouterFields tells which methods take the router login; UPnP asks the network instead. */
function usesRouterFields(m: Method): boolean {
  return m === 'command' || m === 'http' || m === 'script';
}

function CommandFields({ rc, write }: FieldProps) {
  const { t } = useT();
  return (
    <>
      <Field label={t('settings.reconnect.command')} hint={t('settings.reconnect.commandHint')}>
        <TextInput
          dir="ltr"
          spellCheck={false}
          value={rc.command ?? ''}
          placeholder="/usr/local/bin/reconnect.sh"
          onChange={(e) => write({ command: e.target.value })}
        />
      </Field>
      <Field label={t('settings.reconnect.args')} hint={t('settings.reconnect.argsHint')}>
        <LinesArea rows={3} lines={rc.args} placeholder="--router%%router%%" onLines={(v) => write({ args: v })} />
      </Field>
    </>
  );
}

function UPnPFields({ rc, write }: FieldProps) {
  const { t } = useT();
  return (
    <>
      <Field
        label={t('settings.reconnect.upnpLocation')}
        hint={[t('settings.reconnect.upnpState'), t('settings.reconnect.upnpLocationHint')]}
      >
        <TextInput
          dir="ltr"
          spellCheck={false}
          value={rc.upnpLocation ?? ''}
          placeholder="http://192.168.1.1:5000/rootDesc.xml"
          onChange={(e) => write({ upnpLocation: e.target.value })}
        />
      </Field>
    </>
  );
}

function ScriptFields({ rc, write }: FieldProps) {
  const { t } = useT();
  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('settings.reconnect.interpreter')} hint={t('settings.reconnect.interpreterHint')}>
          <TextInput
            dir="ltr"
            spellCheck={false}
            value={rc.interpreter ?? ''}
            placeholder="/bin/sh"
            onChange={(e) => write({ interpreter: e.target.value })}
          />
        </Field>
        <Field
          label={t('settings.reconnect.interpreterArgs')}
          hint={t('settings.reconnect.interpreterArgsHint')}
        >
          <LinesArea
            rows={2}
            lines={rc.interpreterArgs}
            placeholder="-e"
            onLines={(v) => write({ interpreterArgs: v })}
          />
        </Field>
      </div>
      <Field label={t('settings.reconnect.script')} hint={t('settings.reconnect.scriptHint')}>
        <TextArea
          dir="ltr"
          rows={8}
          spellCheck={false}
          value={rc.script ?? ''}
          onChange={(e) => write({ script: e.target.value })}
        />
      </Field>
    </>
  );
}

/**
 * RouterFields holds the router login, the %%router%%, %%username%% and
 * %%password%% variables of the three methods that talk to the router.
 */
function RouterFields({ rc, write }: FieldProps) {
  const { t } = useT();
  const [finding, setFinding] = useState(false);
  const [found, setFound] = useState('');
  const [failed, setFailed] = useState('');

  // An untouched draft keeps the mask, which tells the save "unchanged".
  const stored = rc.password === REDACTED;

  async function find() {
    setFinding(true);
    setFound('');
    setFailed('');
    try {
      const r = await fetch('/api/reconnect/router');
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      const a = (await r.json()) as RouterAddress;
      write({ router: a.address });
      setFound(
        a.interface
          ? t('settings.reconnect.routerFoundVia', { address: a.address, iface: a.interface })
          : t('settings.reconnect.routerFound', { address: a.address }),
      );
    } catch (e) {
      // A failed lookup is named rather than silent.
      setFailed(t('settings.reconnect.routerFailed', { reason: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setFinding(false);
    }
  }

  return (
    <>
      <div className="flex items-end gap-3">
        <div className="min-w-0 flex-1">
          {/* The button sits outside the Field, whose label would pass it clicks. */}
          <Field label={t('settings.reconnect.router')} hint={t('settings.reconnect.routerHint')}>
            <TextInput
              dir="ltr"
              spellCheck={false}
              value={rc.router ?? ''}
              placeholder="192.168.1.1"
              onChange={(e) => write({ router: e.target.value })}
            />
          </Field>
        </div>
        <Button
          kind="secondary"
          disabled={finding}
          onClick={find}
          icon={<IconSearch width={16} height={16} />}
        >
          {finding ? t('settings.reconnect.routerFinding') : t('settings.reconnect.routerFind')}
        </Button>
      </div>
      {found && <StateLine tone="muted">{found}</StateLine>}
      {failed && <StateLine tone="warn">{failed}</StateLine>}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('settings.reconnect.username')} hint={t('settings.reconnect.usernameHint')}>
          <TextInput
            dir="ltr"
            autoComplete="off"
            spellCheck={false}
            value={rc.username ?? ''}
            onChange={(e) => write({ username: e.target.value })}
          />
        </Field>
        <Field label={t('settings.reconnect.password')} hint={t('settings.reconnect.passwordHint')}>
          <TextInput
            type="password"
            dir="ltr"
            autoComplete="new-password"
            value={stored ? '' : (rc.password ?? '')}
            placeholder={stored ? t('settings.reconnect.passwordStored') : ''}
            onChange={(e) => write({ password: e.target.value })}
          />
        </Field>
      </div>
    </>
  );
}

function RequestFields({ rc, write }: FieldProps) {
  const { t } = useT();
  const [importing, setImporting] = useState(false);
  const rows = rc.requests ?? [];

  const update = (i: number, fields: Partial<ReconnectRequest>) =>
    write({ requests: rows.map((q, n) => (n === i ? { ...q, ...fields } : q)) });

  const move = (i: number, by: number) => {
    const to = i + by;
    if (to < 0 || to >= rows.length) return;
    const next = [...rows];
    [next[i], next[to]] = [next[to], next[i]];
    write({ requests: next });
  };

  return (
    <div className="flex flex-col gap-4">
      <SectionTitle
        right={
          <div className="flex items-center gap-2">
            <Button kind="secondary" onClick={() => setImporting(!importing)}>
              {t('settings.reconnect.import')}
            </Button>
            {/* Secondary: the one primary button here is "Run it now". */}
            <Button
              kind="secondary"
              icon={<IconPlus width={16} height={16} />}
              onClick={() => write({ requests: [...rows, { method: 'GET', url: '' }] })}
            >
              {t('settings.reconnect.requestAdd')}
            </Button>
          </div>
        }
      >
        {t('settings.reconnect.requests')}
        <InfoBubble tip={t('settings.reconnect.requestsHint')} />
      </SectionTitle>

      {importing && (
        <ImportPanel
          onClose={() => setImporting(false)}
          onUse={(requests) => {
            write({ requests });
            setImporting(false);
          }}
        />
      )}

      {rows.length === 0 ? (
        <p className="py-4 text-center text-sm text-carbon-textSub">{t('settings.reconnect.requestsEmpty')}</p>
      ) : (
        <ul className="flex flex-col gap-3">
          {rows.map((q, i) => (
            <RequestRow
              key={i}
              index={i}
              last={i === rows.length - 1}
              row={q}
              onChange={(fields) => update(i, fields)}
              onMove={(by) => move(i, by)}
              onRemove={() => write({ requests: rows.filter((_, n) => n !== i) })}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

/** The verbs a router script uses; free entry would let a typo reach the router. */
const VERBS = ['GET', 'POST', 'PUT', 'DELETE', 'HEAD'];

function RequestRow({
  index,
  last,
  row,
  onChange,
  onMove,
  onRemove,
}: {
  index: number;
  last: boolean;
  row: ReconnectRequest;
  onChange: (fields: Partial<ReconnectRequest>) => void;
  onMove: (by: number) => void;
  onRemove: () => void;
}) {
  const { t } = useT();
  return (
    <li className="glim-well flex flex-col gap-3 p-4">
      <div className="flex items-center gap-3">
        <span className="glim-num text-xs font-medium text-carbon-textSub">
          {t('settings.reconnect.requestStep', { n: index + 1 })}
        </span>
        <span className="flex-1" />
        {/* `labelled`, so the actions follow the Beschriftung setting. */}
        <div className="flex items-center gap-1.5">
          <IconBadge
            labelled
            icon={<IconArrowUp width={16} height={16} />}
            hue={index}
            title={t('settings.reconnect.requestUp')}
            aria-label={t('settings.reconnect.requestUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            labelled
            icon={<IconArrowDown width={16} height={16} />}
            hue={index}
            title={t('settings.reconnect.requestDown')}
            aria-label={t('settings.reconnect.requestDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            labelled
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={t('settings.reconnect.requestRemove')}
            aria-label={t('settings.reconnect.requestRemove')}
            onClick={onRemove}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[8rem_1fr]">
        <Field label={t('settings.reconnect.requestMethod')}>
          <Dropdown
            label={t('settings.reconnect.requestMethod')}
            value={(row.method || 'GET').toUpperCase()}
            onChange={(v) => onChange({ method: v })}
            options={VERBS.map((v) => ({ value: v, label: v }))}
          />
        </Field>
        <Field label={t('settings.reconnect.requestUrl')} hint={t('settings.reconnect.requestUrlHint')}>
          <TextInput
            dir="ltr"
            spellCheck={false}
            value={row.url}
            placeholder="http://%%router%%/login.cgi"
            onChange={(e) => onChange({ url: e.target.value })}
          />
        </Field>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('settings.reconnect.requestHeaders')} hint={t('settings.reconnect.requestHeadersHint')}>
          <HeadersArea headers={row.headers} onHeaders={(h) => onChange({ headers: h })} />
        </Field>
        <Field label={t('settings.reconnect.requestBody')} hint={t('settings.reconnect.requestBodyHint')}>
          <TextArea
            dir="ltr"
            rows={3}
            spellCheck={false}
            value={row.body ?? ''}
            placeholder="user=%%username%%&pass=%%password%%"
            onChange={(e) => onChange({ body: e.target.value })}
          />
        </Field>
      </div>
    </li>
  );
}

/**
 * ImportPanel reads a LiveHeader script inline, showing what mapped and every
 * refused line side by side. A script with a refused line cannot be used, since
 * half a router script logs in without rebooting.
 */
function ImportPanel({
  onClose,
  onUse,
}: {
  onClose: () => void;
  onUse: (requests: ReconnectRequest[]) => void;
}) {
  const { t } = useT();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);
  const [failed, setFailed] = useState('');

  async function read() {
    setBusy(true);
    setFailed('');
    try {
      const r = await fetch('/api/reconnect/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      });
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      setResult((await r.json()) as ImportResult);
    } catch (e) {
      setFailed(t('settings.reconnect.importFailed', { reason: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setBusy(false);
    }
  }

  const mapped = result?.requests ?? [];
  const problems = result?.problems ?? [];
  const refused = Boolean(result?.error);

  return (
    <div className="glim-well flex flex-col gap-4 p-4">
      <Field label={t('settings.reconnect.importLabel')} hint={t('settings.reconnect.importHint')}>
        <TextArea
          dir="ltr"
          rows={8}
          spellCheck={false}
          value={text}
          placeholder={'[[[HSRC]]]\nGET /login.cgi HTTP/1.1\nHost: %%%routerip%%%\n[[[/HSRC]]]'}
          // Any edit invalidates the last reading, so Use never applies stale requests.
          onChange={(e) => {
            setText(e.target.value);
            setResult(null);
          }}
        />
      </Field>

      {/* Read or use, never both; an edit drops the result. */}
      <div className="flex items-center gap-2">
        <span className="flex-1" />
        <Button kind="ghost" onClick={onClose}>
          {t('settings.reconnect.importClose')}
        </Button>
        {result ? (
          <Button disabled={refused || mapped.length === 0} onClick={() => onUse(mapped)}>
            {t('settings.reconnect.importUse', { n: mapped.length })}
          </Button>
        ) : (
          <Button disabled={busy || text.trim() === ''} onClick={read}>
            {busy ? t('settings.reconnect.importReading') : t('settings.reconnect.importRead')}
          </Button>
        )}
      </div>

      {failed && <StateLine tone="fail">{failed}</StateLine>}

      {result && (
        <div className="flex flex-col gap-3">
          <p className="text-xs text-carbon-textSub">
            {t('settings.reconnect.importMapped', { n: mapped.length })}
            {problems.length > 0 && (
              <span className="text-statusWarn">
                {' · '}
                {t('settings.reconnect.importRefusedCount', { n: problems.length })}
              </span>
            )}
          </p>

          {mapped.length > 0 && (
            <ul className="flex flex-col gap-1">
              {mapped.map((q, i) => (
                <li key={i} className="text-xs">
                  <span className="glim-num me-2 text-carbon-textMuted">{(q.method || 'GET').toUpperCase()}</span>
                  <span dir="ltr" className="break-all text-carbon-textSub">
                    {q.url}
                  </span>
                </li>
              ))}
            </ul>
          )}

          {problems.length > 0 && (
            // Every refusal with its line number and the line itself.
            <ul className="flex max-h-56 flex-col gap-2 overflow-y-auto">
              {problems.map((p, i) => (
                <li key={i} className="text-xs">
                  <span className="glim-num text-carbon-textMuted">
                    {t('settings.reconnect.importLine', { n: p.line })}
                  </span>
                  {/* A real space, so a screen reader does not run number and line together. */}
                  {' '}
                  <span dir="ltr" className="ms-2 break-all text-carbon-textSub">
                    {p.text}
                  </span>
                  <span className="mt-0.5 block text-statusWarn">{p.why}</span>
                </li>
              ))}
            </ul>
          )}

          {refused && <StateLine tone="warn">{t('settings.reconnect.importBlocked')}</StateLine>}
        </div>
      )}
    </div>
  );
}

function CheckFields({ rc, write }: FieldProps) {
  const { t } = useT();
  const url = (rc.checkUrl ?? '').trim();
  const preset = CHECK_PRESETS.find((p) => p.url === url);

  const interval = rc.intervalSeconds;
  const timeout = rc.timeoutSeconds;

  return (
    <>
      <Field label={t('settings.reconnect.checkUrl')} hint={t('settings.reconnect.checkUrlHint')}>
        <TextInput
          dir="ltr"
          spellCheck={false}
          value={rc.checkUrl ?? ''}
          placeholder="https://api.ipify.org"
          onChange={(e) => write({ checkUrl: e.target.value })}
        />
      </Field>

      {/* No preset is selected while the URL is somebody's own. */}
      <FieldGroup label={t('settings.reconnect.checkPresets')} hint={t('settings.reconnect.checkPresetsHint')}>
        <Tabs
          label={t('settings.reconnect.checkPresets')}
          size="sm"
          active={preset?.id ?? null}
          onSelect={(id) => write({ checkUrl: CHECK_PRESETS.find((p) => p.id === id)?.url ?? '' })}
          items={CHECK_PRESETS.map((p) => ({ id: p.id, label: p.id, title: p.url }))}
        />
      </FieldGroup>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Field label={t('settings.reconnect.interval')} hint={t('settings.reconnect.intervalHint')}>
            <NumberInput
              value={interval}
              min={INTERVAL.lo}
              max={INTERVAL.hi}
              onValue={(v) => write({ intervalSeconds: v })}
            />
          </Field>
          <Clamped value={interval} band={INTERVAL} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Field label={t('settings.reconnect.timeout')} hint={t('settings.reconnect.timeoutHint')}>
            <NumberInput
              value={timeout}
              min={TIMEOUT.lo}
              max={TIMEOUT.hi}
              onValue={(v) => write({ timeoutSeconds: v })}
            />
          </Field>
          <Clamped value={timeout} band={TIMEOUT} />
        </div>
      </div>
    </>
  );
}

/**
 * Clamped shows what a number outside the band will be stored as, mirroring
 * Sanitize: zero means unset and comes back as the default, not the floor.
 */
function Clamped({ value, band }: { value: number; band: { lo: number; hi: number; fallback: number } }) {
  const { t } = useT();
  const folded =
    value <= 0 ? band.fallback : value < band.lo ? band.lo : value > band.hi ? band.hi : 0;
  if (!folded) return null;
  return <StateLine tone="warn">{t('settings.reconnect.clamped', { n: folded })}</StateLine>;
}

/** RunPanel shows the state, the run button and when reconnects happen on their own. */
function RunPanel({ state, disabled }: { state: ReconnectState | null; disabled: boolean }) {
  const { t } = useT();
  // The state and the run use the saved configuration, which differs while
  // the form is dirty.
  const { dirty } = useDraft();
  const reasonText = useReasonText(state);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<RunResult | null>(null);
  const [note, setNote] = useState<{ tone: Tone; text: string } | null>(null);

  const busy = running || Boolean(state?.busy);

  async function run() {
    setRunning(true);
    setResult(null);
    setNote(null);
    try {
      const res = await runReconnect();
      if (res) setResult(res);
      else setNote({ tone: 'muted', text: t('settings.reconnect.runBusy') });
    } catch (e) {
      setNote({ tone: 'fail', text: t('settings.reconnect.runFailed', { reason: String(e).replace(/^Error:\s*/, '') }) });
    } finally {
      setRunning(false);
    }
  }

  return (
    <>
      <div className="flex flex-wrap items-center gap-4">
        <Fact
          label={state?.configured ? t('settings.reconnect.stateConfigured') : t('settings.reconnect.stateNotConfigured')}
          tone={state === null ? 'muted' : state.configured ? 'ok' : 'muted'}
        />
        {/* Only once something could run, or "not configured" and "ready" read
            as one sentence. */}
        {state?.configured && (
          <Fact
            label={busy ? t('settings.reconnect.stateBusy') : t('settings.reconnect.stateIdle')}
            tone={busy ? 'live' : 'muted'}
          />
        )}
        <span className="flex-1" />
        <Button
          onClick={run}
          disabled={busy || disabled}
          icon={<IconRetry width={16} height={16} />}
        >
          {running ? t('settings.reconnect.running') : t('settings.reconnect.runNow')}
        </Button>
      </div>

      {/* While dirty this replaces the readiness line, which describes the saved
          configuration. */}
      {dirty && <StateLine tone="warn">{t('settings.reconnect.runUsesSaved')}</StateLine>}

      {/* Validate's own words, pointing at the missing field. */}
      {!dirty && state && !state.configured && !disabled && reasonText && (
        <StateLine tone="warn">{t('settings.reconnect.notReady', { reason: reasonText })}</StateLine>
      )}
      {state === null && <StateLine tone="muted">{t('settings.reconnect.stateUnreadable')}</StateLine>}

      {result && (
        <p className="text-xs text-statusOk">
          <span dir="ltr">
            {t('settings.reconnect.runMoved', { from: result.oldIp, to: result.newIp })}
          </span>
          <span className="glim-num text-carbon-textMuted">
            {' · '}
            {t('settings.reconnect.runDetail', {
              n: result.checks,
              secs: (result.tookMs / 1000).toFixed(1),
            })}
          </span>
        </p>
      )}
      {note && <StateLine tone={note.tone}>{note.text}</StateLine>}

      <div className="flex items-center pt-1 text-xs text-carbon-textSub">
        {t('settings.reconnect.policy')}
        <InfoBubble tip={t('settings.reconnect.policyHint')} />
      </div>
    </>
  );
}

/** Fact is one word of state behind a coloured dot. */
function Fact({ label, tone }: { label: string; tone: 'ok' | 'muted' | 'live' }) {
  const dot =
    tone === 'ok' ? 'bg-statusOkSolid' : tone === 'live' ? 'bg-accent glim-live' : 'bg-carbon-surface3';
  return (
    <span className="flex items-center gap-2 text-xs text-carbon-textSub">
      <span className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${dot}`} />
      {label}
    </span>
  );
}

type Tone = 'muted' | 'warn' | 'fail';

/** StateLine is a fact in a state hue; explanations sit behind the (i). */
function StateLine({ tone, children }: { tone: Tone; children: ReactNode }) {
  const cls = tone === 'fail' ? 'text-statusFail' : tone === 'warn' ? 'text-statusWarn' : 'text-carbon-textMuted';
  return <p className={`text-xs ${cls}`}>{children}</p>;
}

interface FieldProps {
  rc: ReconnectConfig;
  write: (fields: Partial<ReconnectConfig>) => void;
}

/**
 * LinesArea edits a list one line at a time. It keeps its own text, because a
 * value joined from the array drops the empty line Enter makes; it re-seeds only
 * when the array disagrees with the text.
 */
function LinesArea({
  lines,
  rows,
  placeholder,
  onLines,
}: {
  lines?: string[];
  rows: number;
  placeholder?: string;
  onLines: (v: string[]) => void;
}) {
  const [text, setText] = useState(() => (lines ?? []).join('\n'));

  useEffect(() => {
    const incoming = lines ?? [];
    if (splitLines(text).join('\n') !== incoming.join('\n')) setText(incoming.join('\n'));
    // Follows the array arriving from elsewhere, not the typing that made it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lines]);

  return (
    <TextArea
      dir="ltr"
      rows={rows}
      spellCheck={false}
      value={text}
      placeholder={placeholder}
      onChange={(e) => {
        setText(e.target.value);
        onLines(splitLines(e.target.value));
      }}
    />
  );
}

/**
 * HeadersArea edits a request's headers as "Name: value" lines, keeping its own
 * text like LinesArea, since a name typed without its colon yet would vanish.
 */
function HeadersArea({
  headers,
  onHeaders,
}: {
  headers?: Record<string, string>;
  onHeaders: (h?: Record<string, string>) => void;
}) {
  const [text, setText] = useState(() => headersToText(headers));

  useEffect(() => {
    if (headersToText(textToHeaders(text)) !== headersToText(headers)) setText(headersToText(headers));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [headers]);

  return (
    <TextArea
      dir="ltr"
      rows={3}
      spellCheck={false}
      value={text}
      placeholder="Host: %%router%%"
      onChange={(e) => {
        setText(e.target.value);
        onHeaders(textToHeaders(e.target.value));
      }}
    />
  );
}

function splitLines(s: string): string[] {
  return s
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean);
}

/** headersToText writes "Name: value" lines, sorted so the text does not reshuffle. */
function headersToText(h?: Record<string, string>): string {
  if (!h) return '';
  return Object.keys(h)
    .sort()
    .map((k) => `${k}: ${h[k]}`)
    .join('\n');
}

function textToHeaders(s: string): Record<string, string> | undefined {
  const out: Record<string, string> = {};
  for (const line of s.split('\n')) {
    const at = line.indexOf(':');
    if (at <= 0) continue; // a line with no name is not a header; the server drops it too
    const name = line.slice(0, at).trim();
    if (name) out[name] = line.slice(at + 1).trim();
  }
  return Object.keys(out).length > 0 ? out : undefined;
}
