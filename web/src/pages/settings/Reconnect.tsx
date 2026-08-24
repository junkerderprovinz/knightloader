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
import { Tabs } from '../../components/Tabs';
import {
  IconArrowDown,
  IconArrowUp,
  IconClose,
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

/**
 * The reconnect page: how this box asks the router for a new public address.
 *
 * Four decisions here are not layout.
 *
 * THE METHOD DECIDES WHAT IS ON SCREEN. Five methods share one page, and the
 * fields of four of them are noise to the fifth. Somebody who picked UPnP is
 * looking at a page with no interpreter, no request list and no program path on
 * it, because every one of those would read as a thing they had failed to fill
 * in. The values stay in the draft while they are hidden, so switching methods
 * to look at another one and coming back loses nothing.
 *
 * THE PASSWORD IS NEVER RENDERED BACK. The server sends the placeholder
 * "********" in place of a stored router password; the box shows empty and the
 * draft keeps the placeholder, which is what the save merges back into the real
 * one. Painting the placeholder into the field would teach people their password
 * is eight characters long, and clearing the field on load would wipe it on the
 * next save of any other setting on this page.
 *
 * THE CHECK URL HAS NO DEFAULT, ON PURPOSE. A self-hosted download manager must
 * not start reporting its address to a service nobody chose. The preset strip is
 * a shortcut for people who do not care which one, not a default.
 *
 * THE RUN POLICY IS DESCRIBED, NOT IMPLEMENTED. Automatic reconnects are fired
 * by internal/app/app_dispatch.go and nowhere else. This page has one button
 * that runs one reconnect because the user pressed it; a second trigger living
 * in the interface would fire on a page nobody has open.
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

/** GET /api/reconnect. `reason` is Validate's own sentence, not a second opinion. */
interface ReconnectState {
  busy: boolean;
  configured: boolean;
  /** The server's English sentence. The fallback, not the first choice. */
  reason?: string;
  /** The same fact as a value, which is the half this side can translate. */
  reasonCode?: string;
  reasonN?: number;
  reasonMethod?: string;
  reasonVar?: string;
}

/**
 * What is missing, in the reader's language.
 *
 * The server sends both halves and this prefers the code, because the sentence
 * is English and the interface is not. An unrecognised code falls back to the
 * sentence rather than to nothing: a server that grows a tenth reason before
 * this file learns the word for it should still say something true, in the
 * wrong language, instead of leaving a blank where the explanation was.
 */
function useReasonText(state: ReconnectState | null): string {
  const { t } = useT();
  if (!state) return '';
  const code = state.reasonCode;
  if (!code) return state.reason ?? '';
  const key = `settings.reconnect.reason.${code}` as never;
  const text = t(key, { n: state.reasonN ?? 0, method: state.reasonMethod ?? '', var: state.reasonVar ?? '' });
  // t() answers with the key itself when it has no entry, which on screen is a
  // dotted identifier and not an explanation.
  return text === key ? (state.reason ?? '') : text;
}

/** POST /api/reconnect. */
interface RunResult {
  oldIp: string;
  newIp: string;
  checks: number;
  tookMs: number;
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

/**
 * What the server sends instead of a stored router password. Kept as a constant
 * because it is a protocol value shared with reconnect.RedactedPassword, not a
 * string somebody may prettify.
 */
const REDACTED = '********';

/**
 * The band reconnect.Sanitize folds these two numbers into, and the value it
 * uses when one of them is unset. Both are shown rather than enforced quietly,
 * so nobody meets them by typing 3600 and finding 900 there after the save.
 */
const INTERVAL = { lo: 1, hi: 60, fallback: 5 };
const TIMEOUT = { lo: 5, hi: 900, fallback: 120 };

const DEFAULTS: ReconnectConfig = {
  method: 'none',
  intervalSeconds: INTERVAL.fallback,
  timeoutSeconds: TIMEOUT.fallback,
};

/**
 * The five methods, in the order the Go constants declare them.
 *
 * Every glyph is one the app already draws for the same idea, which is the rule
 * the settings tab bar follows: the globe for something going out over the
 * network, the two stacked boxes for another device on it, the play arrow for a
 * program being started, and the box with an arrow leaving it for the script
 * that is written out to a file and handed to an interpreter.
 */
const METHODS: { id: Method; icon: ReactNode }[] = [
  { id: 'none', icon: <IconClose width={16} height={16} /> },
  { id: 'command', icon: <IconPlay width={16} height={16} /> },
  { id: 'http', icon: <IconGlobe width={16} height={16} /> },
  { id: 'upnp', icon: <IconInstances width={16} height={16} /> },
  { id: 'script', icon: <IconCollector width={16} height={16} /> },
];

/**
 * The check services offered as a shortcut.
 *
 * Deliberately a handful and deliberately plain-text endpoints: the parser
 * copes with HTML, but a body that is one address is the cheapest and the
 * hardest to misread. The labels are host names, so they are not translated -
 * a domain is not a word.
 */
const CHECK_PRESETS: { id: string; url: string }[] = [
  { id: 'ipify', url: 'https://api.ipify.org' },
  { id: 'icanhazip', url: 'https://icanhazip.com' },
  { id: 'ifconfig.me', url: 'https://ifconfig.me/ip' },
  { id: 'ipinfo.io', url: 'https://ipinfo.io/ip' },
  { id: 'checkip.amazonaws.com', url: 'https://checkip.amazonaws.com' },
];

/** The settings type in lib/api.ts does not name `reconnect` yet, and the draft
 *  carries it regardless - see the note on SettingsDraft.cfg. These two casts are
 *  the whole of that gap. */
function readReconnect(cfg: unknown): ReconnectConfig {
  return { ...DEFAULTS, ...((cfg as { reconnect?: ReconnectConfig }).reconnect ?? {}) };
}

export function Reconnect() {
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

  // Polled rather than read once: `busy` is true for as long as a run takes, a
  // run can be started by a hoster limit rather than by this page, and a strip
  // that says "idle" through the whole of one is worse than no strip at all.
  useEffect(() => {
    let alive = true;
    const read = async () => {
      try {
        const r = await fetch('/api/reconnect');
        if (!r.ok) throw new Error(String(r.status));
        const s = (await r.json()) as ReconnectState;
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
    <div className="flex flex-col gap-10">
      <Card className="flex flex-col gap-5">
        <SectionTitle hue={0}>{t('settings.reconnect.setupTitle')}</SectionTitle>
        {/* FieldGroup, not Field: a Field is a `<label>` and hands its clicks to
            the first control inside it, which for a tab strip is the first tab.
            See ui.tsx. */}
        <FieldGroup layout="row" label={t('settings.reconnect.method')} hint={t('settings.reconnect.methodHint')}>
          <Tabs
            label={t('settings.reconnect.method')}
            variant="well"
            size="sm"
            className="w-fit"
            active={rc.method}
            onSelect={(id) => write({ method: id as Method })}
            items={METHODS.map((m) => ({
              id: m.id,
              // No cast: the ids are a union of literals, so the template
              // resolves to five real keys and a sixth method would not compile.
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
        <Card className="flex flex-col gap-5">
          <SectionTitle hue={1}>{t('settings.reconnect.checkTitle')}</SectionTitle>
          <CheckFields rc={rc} write={write} />
        </Card>
      )}

      <Card className="flex flex-col gap-4">
        <SectionTitle hue={2}>{t('settings.reconnect.runTitle')}</SectionTitle>
        <RunPanel state={state} disabled={off} />
      </Card>
    </div>
  );
}

/** Which methods substitute %%router%%, %%username%% and %%password%%. UPnP asks
 *  the network instead of logging in, so offering it a login is offering three
 *  fields that do nothing. */
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
      <StateLine tone="muted">{t('settings.reconnect.upnpState')}</StateLine>
      <Field label={t('settings.reconnect.upnpLocation')} hint={t('settings.reconnect.upnpLocationHint')}>
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
 * The router login, shared by the three methods that speak to the router
 * themselves. The three fields are the %%router%%, %%username%% and %%password%%
 * variables and nothing else, which is why they sit below the method's own
 * fields rather than above them: they are what those fields refer to.
 */
function RouterFields({ rc, write }: FieldProps) {
  const { t } = useT();
  const [finding, setFinding] = useState(false);
  const [found, setFound] = useState('');
  const [failed, setFailed] = useState('');

  // The placeholder is what the server sends in place of a stored password. An
  // untouched draft keeps it, which is what tells the save "unchanged"; the box
  // itself stays empty, so nobody is shown eight characters that are not theirs.
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
      // Named, never silent. A button that fills nothing in and says nothing
      // reads as a broken button rather than as a box with no gateway to read.
      setFailed(t('settings.reconnect.routerFailed', { reason: String(e).replace(/^Error:\s*/, '') }));
    } finally {
      setFinding(false);
    }
  }

  return (
    <>
      <div className="flex items-end gap-3">
        <div className="min-w-0 flex-1">
          {/* The button is OUTSIDE the Field. A `<label>` around both would hand
              a click on the word "Router address" to whichever came first. */}
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

/** The HTTP method's request list, and the LiveHeader import that fills it. */
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
            {/* Secondary, not primary: the one accent-filled button on this page
                is "Run it now", because the accent means activity and adding an
                empty row to a list is not any. Rule 3. */}
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

/** The verbs a router script uses. Free entry is deliberately not offered: a
 *  mistyped verb produces a request the router answers with a parse error, and
 *  the reconnect then reports a status code instead of a missing letter. */
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
    <li className="glim-well group flex flex-col gap-3 p-4">
      <div className="flex items-center gap-3">
        <span className="glim-num text-xs font-medium text-carbon-textSub">
          {t('settings.reconnect.requestStep', { n: index + 1 })}
        </span>
        <span className="flex-1" />
        {/* Secondary actions on hover and on keyboard focus - rule 6. */}
        <div className="flex items-center gap-1.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          <IconBadge
            icon={<IconArrowUp width={14} height={14} />}
            hue={index}
            title={t('settings.reconnect.requestUp')}
            aria-label={t('settings.reconnect.requestUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            icon={<IconArrowDown width={14} height={14} />}
            hue={index}
            title={t('settings.reconnect.requestDown')}
            aria-label={t('settings.reconnect.requestDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            kind="danger"
            icon={<IconTrash width={14} height={14} />}
            hue={index}
            title={t('settings.reconnect.requestRemove')}
            aria-label={t('settings.reconnect.requestRemove')}
            onClick={onRemove}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[8rem_1fr]">
        <Field label={t('settings.reconnect.requestMethod')}>
          <Select value={(row.method || 'GET').toUpperCase()} onChange={(v) => onChange({ method: v })}>
            {VERBS.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </Select>
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
 * The LiveHeader import, inline rather than in a dialog.
 *
 * Both halves of the answer have to be readable at once - what mapped and what
 * did not, each refusal with its line number and the line itself - and that is a
 * two-column read, not something to do in a narrow modal on top of the list it
 * is about to replace.
 *
 * A script with a refused line is not offered for use at all. The parser fills
 * in as far as it got so the mapped half can be seen, but half a router script
 * is a login with no reboot, and that failure surfaces days later as "the
 * address did not change" at three in the morning.
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
          // Any edit invalidates the last reading, so the Use button can never
          // apply requests parsed from text that is no longer on screen.
          onChange={(e) => {
            setText(e.target.value);
            setResult(null);
          }}
        />
      </Field>

      {/* Either read, or use what was read - never both at once. Any edit to the
          text drops the result, so the pair cannot get out of step with what is
          on screen, and the panel keeps to one filled button. */}
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
            // Every refusal, with the line it belongs to and the line itself.
            // A message under a forty-line paste leaves somebody counting rows.
            <ul className="flex max-h-56 flex-col gap-2 overflow-y-auto">
              {problems.map((p, i) => (
                <li key={i} className="text-xs">
                  <span className="glim-num text-carbon-textMuted">
                    {t('settings.reconnect.importLine', { n: p.line })}
                  </span>
                  {/* An explicit space, not only the margin: without it a screen
                      reader reads the number and the line as one word. */}
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

/** The check URL, its shortcuts, and the two waits. */
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

      {/* The same strip as everywhere else, so "one of these is in force" reads
          the way it does on the corner picker. Nothing is filled while the URL is
          somebody's own, which is the honest picture: the presets are a shortcut,
          and no service is chosen for anyone. */}
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
 * What a number outside the band will actually be stored as.
 *
 * Sanitize folds it without saying so, and a field that reads 3600 before a save
 * and 900 after it is the kind of thing people stop trusting the whole page
 * over. It mirrors Sanitize exactly, zero included: zero means "unset" there and
 * comes back as the default rather than as the floor, so reporting the floor
 * here would be a different lie in the same place.
 *
 * This is a fact about the value, not an explanation of the control, so it is on
 * the page rather than behind the (i).
 */
function Clamped({ value, band }: { value: number; band: { lo: number; hi: number; fallback: number } }) {
  const { t } = useT();
  const folded =
    value <= 0 ? band.fallback : value < band.lo ? band.lo : value > band.hi ? band.hi : 0;
  if (!folded) return null;
  return <StateLine tone="warn">{t('settings.reconnect.clamped', { n: folded })}</StateLine>;
}

/**
 * The state strip, the one button, and the truth about when this happens on its
 * own.
 */
function RunPanel({ state, disabled }: { state: ReconnectState | null; disabled: boolean }) {
  const { t } = useT();
  // Everything in this panel is about the SAVED configuration: the state comes
  // from the server, and so does the run. While the form is dirty the two are
  // different things, and saying nothing about that is how somebody picks a
  // method, presses run, and watches the instance do what it was doing before.
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
      const r = await fetch('/api/reconnect', { method: 'POST' });
      if (r.status === 409) {
        // Not a failure. Something else is already doing exactly what this
        // button asks for, and reporting it as an error would have people press
        // it again into the same refusal.
        setNote({ tone: 'muted', text: t('settings.reconnect.runBusy') });
        return;
      }
      if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
      setResult((await r.json()) as RunResult);
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
        {/* Only once something could actually run. Drawn unconditionally, the
            two facts sit side by side in the same grey and read as one
            sentence - "not configured, ready" - which is a contradiction the
            reader has to resolve before noticing they are two separate
            questions. Whether a reconnect is running is not a fact about an
            instance that has none to run. */}
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
          {running ? t('settings.reconnect.running') : t('settings.reconnect.run')}
        </Button>
      </div>

      {/* While the form is dirty this says the one thing that matters, and the
          readiness line below is suppressed: it describes the saved
          configuration, so against an edited form it contradicts what is on
          screen - "switched off" under a method strip pointing at Requests. */}
      {dirty && <StateLine tone="warn">{t('settings.reconnect.runUsesSaved')}</StateLine>}

      {/* Validate's own words, pointing at the field rather than at the method
          strip - which is already set to something, and is not what is missing. */}
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

/** One word of state with a dot in front of it. No box, no border: the dot
 *  carries the colour and the shade carries the separation. */
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

/** A fact about the current state, in one of the state hues. Not an explanation:
 *  those live behind the (i) beside the label. */
function StateLine({ tone, children }: { tone: Tone; children: ReactNode }) {
  const cls = tone === 'fail' ? 'text-statusFail' : tone === 'warn' ? 'text-statusWarn' : 'text-carbon-textMuted';
  return <p className={`text-xs ${cls}`}>{children}</p>;
}

/**
 * The one control the design language has no primitive for, styled to match
 * TextInput exactly. It is a copy of the Select in Connections.tsx, and the two
 * belong in ui.tsx the moment a third page needs one - two copies is a pair, a
 * third is drift nobody can fix in one place.
 */
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

interface FieldProps {
  rc: ReconnectConfig;
  write: (fields: Partial<ReconnectConfig>) => void;
}

/**
 * A list edited one line at a time.
 *
 * The text is held here rather than derived from the array on every keystroke,
 * and that is the whole reason this component exists. A textarea whose value is
 * `lines.join('\n')` eats the Enter key: the empty line it produces is dropped
 * on the way out and the cursor jumps back to the end of the line above, so a
 * second argument can never be typed. Re-seeding only when the array disagrees
 * with what this text parses to keeps an outside change visible without ever
 * rewriting a keystroke under the cursor.
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
    // `text` is deliberately not a dependency: this effect is about the array
    // arriving from somewhere else, not about the typing that produced it.
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
 * The headers of one request, as the "Name: value" lines they were recorded as.
 *
 * Same arrangement as LinesArea, and here the round trip is worse than a lost
 * newline: a name typed one letter at a time has no colon yet, so the map is
 * empty, so the letter disappears as it is typed.
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

/** Headers are edited as "Name: value" lines, which is how they were recorded
 *  and how every router script in the wild writes them. Sorted by name, so the
 *  text does not reshuffle itself when the map is rebuilt. */
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
