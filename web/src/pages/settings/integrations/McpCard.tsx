import { useCallback, useEffect, useId, useState, type ReactNode } from 'react';
import { BrandPicker, PickerSearch, brandMatches, type Brand } from '../../../components/BrandPicker';
import {
  Button,
  Card,
  EmptyState,
  IconBadge,
  InfoBubble,
  LabelBadge,
  Modal,
  SectionTitle,
  TextInput,
} from '../../../components/ui';
import { Tabs } from '../../../components/Tabs';
import {
  type McpClientRow,
  type McpOverview,
  type TokenScope,
  createMcpClient,
  fetchMcp,
  removeMcpClient,
} from '../../../lib/api';
import { appAddress } from '../../../lib/basePath';
import { copyToClipboard } from '../../../lib/clipboard';
import { fmtDate } from '../../../lib/format';
import { useT, type TranslationKey } from '../../../lib/i18n';
import {
  IconAdd,
  IconBack,
  IconCheck,
  IconCheckDrawn,
  IconClose,
  IconCode,
  IconCopy,
  IconLink,
  IconTrash,
} from '../../../lib/icons';
import { CLIENT_MARKS } from '../../../lib/mcpClientMarks';
import {
  CLOUD_CLIENTS,
  KIND_LABEL,
  LOCAL_CLIENTS,
  OTHER_CLIENT,
  clientById,
  snippetFor,
  type ConfigPaths,
  type McpClient,
} from '../../../lib/mcpClients';
import { TOKEN_PLACEHOLDER, TOKEN_VARIABLE, type McpSnippetInput } from '../../../lib/mcpSnippets';
import { useToast } from '../../../lib/toast';
import { SettingRow, SubHead } from '../controls';
import { PRESET_LABEL, scopesLabel } from '../access/tokenScopes';

// The second card of the Integrations page, after the VPN's.
const HUE = 1;

/** How often the card asks again while a new client has not called yet. */
const FIRST_CALL_POLL_MS = 3000;

/** The rights a client can be given; settings stay out of every one. */
type McpPreset = 'read' | 'addRead' | 'control';

const PRESET_SCOPES: Record<McpPreset, TokenScope[]> = {
  read: ['read'],
  addRead: ['read', 'add'],
  control: ['read', 'add', 'control'],
};

const MCP_PRESET_LABEL: Record<McpPreset, TranslationKey> = {
  read: PRESET_LABEL.read,
  addRead: PRESET_LABEL.addRead,
  control: 'settings.access.mcp.presetControl',
};

const OS_PATHS: [keyof ConfigPaths, string][] = [
  ['windows', 'Windows'],
  ['macos', 'macOS'],
  ['linux', 'Linux'],
  ['unix', 'macOS, Linux'],
];

/** withOwnIds gives a mark's gradients and masks ids of their own, because
 *  one mark can stand on a tile, a row and in a window at once. */
function withOwnIds(svg: string, suffix: string): string {
  return svg
    .replace(/id="([^"]+)"/g, `id="$1-${suffix}"`)
    .replace(/url\(#([^)]+)\)/g, `url(#$1-${suffix})`)
    .replace(/href="#([^"]+)"/g, `href="#$1-${suffix}"`);
}

/** ClientMark draws a client's mark, and the link glyph for Other client. */
export function ClientMark({ client }: { client: McpClient }) {
  const suffix = useId().replace(/[^a-zA-Z0-9]/g, '');
  const mark = client.mark ? CLIENT_MARKS[client.mark] : undefined;
  if (!mark) return <IconLink width={18} height={18} />;
  const rest = withOwnIds(mark.rest, suffix);
  const html = mark.hover
    ? rest.replace('<svg ', '<svg class="glim-mark-rest" ') +
      withOwnIds(mark.hover, `${suffix}h`).replace('<svg ', '<svg class="glim-mark-hover" ')
    : rest;
  return (
    <span
      className={`contents ${client.lift ? 'glim-mark-lift' : ''}`}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

/**
 * McpCard is where an AI client is set up for /mcp: empty until one is added,
 * then one row per client with its token, its state, its configuration and the
 * way to remove it. The token is shown once, in the window that makes it,
 * since the server keeps only a fingerprint.
 */
export function McpCard() {
  const { t } = useT();
  const { toast } = useToast();
  const [data, setData] = useState<McpOverview | null>(null);
  const [failed, setFailed] = useState(false);
  const [picking, setPicking] = useState(false);
  const [viewing, setViewing] = useState<McpClientRow | null>(null);
  const [removing, setRemoving] = useState<McpClientRow | null>(null);
  const [busy, setBusy] = useState(false);
  // A client added in this visit, until its window closes: the card asks for
  // its first call while the window waits for it.
  const [waitingFor, setWaitingFor] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setData(await fetchMcp());
      setFailed(false);
    } catch {
      setFailed(true);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const waiting = data?.clients.find((c) => c.id === waitingFor);
  const stillWaiting = waitingFor !== null && !waiting?.lastCall;
  useEffect(() => {
    if (!stillWaiting) return;
    const timer = window.setInterval(() => void load(), FIRST_CALL_POLL_MS);
    return () => window.clearInterval(timer);
  }, [stillWaiting, load]);

  const base = { origin: appAddress(), endpointPath: data?.path ?? '/mcp' };
  const clients = data?.clients ?? [];

  async function doRemove(row: McpClientRow) {
    setBusy(true);
    try {
      await removeMcpClient(row.id);
      setRemoving(null);
      await load();
    } catch (e) {
      toast(t('settings.access.mcp.removeFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
    } finally {
      setBusy(false);
    }
  }

  const nameOf = (c: McpClient) => (c === OTHER_CLIENT ? t('settings.access.mcp.other') : c.name);
  // A client's rights read as the choice they were made with, or as the
  // token card words any other set.
  const rightsLabel = (scopes: TokenScope[]) => {
    const preset = (Object.keys(PRESET_SCOPES) as McpPreset[]).find(
      (p) => PRESET_SCOPES[p].join() === scopes.join(),
    );
    return preset ? t(MCP_PRESET_LABEL[preset]) : scopesLabel(t, scopes);
  };

  return (
    <>
      <Card hue={HUE} className="flex flex-col gap-3">
        <SectionTitle
          hint={t('settings.access.mcp.intro')}
          right={
            <Button kind="secondary" icon={<IconAdd width={16} height={16} />} onClick={() => setPicking(true)}>
              {t('settings.access.mcp.add')}
            </Button>
          }
        >
          {t('settings.access.mcp.title')}
        </SectionTitle>
        {failed && <p className="text-sm text-statusWarn">{t('settings.access.mcp.loadFailed')}</p>}
        {clients.length === 0 ? (
          <EmptyState
            nested
            icon={<IconLink width={26} height={26} />}
            title={t('settings.access.mcp.empty')}
            hint={t('settings.access.mcp.emptyHint')}
          />
        ) : (
          <div className="flex flex-col divide-y divide-carbon-border/40">
            {clients.map((row) => {
              const client = clientById(row.kind);
              return (
                <div
                  key={row.id}
                  className="glim-row flex flex-wrap items-center gap-x-3 gap-y-2 py-2.5 first:pt-0 last:pb-0"
                >
                  <span className="glim-client-mark" aria-hidden>
                    <ClientMark client={client} />
                  </span>
                  <div className="min-w-[10rem] flex-1">
                    <div className="truncate text-sm text-carbon-text">{nameOf(client)}</div>
                    <div className="text-meta text-carbon-textMuted">
                      {row.tokenGone ? '' : `${row.tokenName} · `}
                      {row.lastCall
                        ? t('settings.access.mcp.lastCall', { when: fmtDate(row.lastCall) })
                        : t('settings.access.mcp.noCall')}
                    </div>
                  </div>
                  <div className="ms-auto flex shrink-0 items-center gap-3">
                    {row.tokenGone ? (
                      <LabelBadge
                        tone="fail"
                        label={t('settings.access.mcp.tokenGone')}
                        tip={t('settings.access.mcp.tokenGoneHint')}
                      />
                    ) : (
                      <LabelBadge label={rightsLabel(row.scopes)} hue={HUE} />
                    )}
                    {/* `labelled`, so the actions follow the Beschriftung setting. */}
                    <IconBadge
                      labelled
                      hue={HUE}
                      icon={<IconCode width={16} height={16} />}
                      title={t('settings.access.mcp.config')}
                      aria-label={t('settings.access.mcp.config')}
                      onClick={() => setViewing(row)}
                    />
                    <IconBadge
                      labelled
                      hue={HUE}
                      icon={<IconTrash width={16} height={16} />}
                      title={t('settings.access.mcp.remove')}
                      aria-label={t('settings.access.mcp.remove')}
                      onClick={() => setRemoving(row)}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>

      {picking && (
        <AddClientWindow
          base={base}
          waiting={waiting}
          onCreated={(row) => {
            setWaitingFor(row.id);
            void load();
          }}
          onClose={() => {
            setPicking(false);
            setWaitingFor(null);
          }}
        />
      )}

      {viewing && (
        <Modal
          title={nameOf(clientById(viewing.kind))}
          hue={HUE}
          wide
          onClose={() => setViewing(null)}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="primary" labelled icon={<IconCheck />} title={t('settings.access.tokens.done')} onClick={() => setViewing(null)} />
            </>
          }
        >
          <div className="flex flex-col gap-4">
            <p className="text-sm text-carbon-textSub">{t('settings.access.mcp.configHint')}</p>
            <ClientSetup client={clientById(viewing.kind)} input={{ ...base, token: TOKEN_PLACEHOLDER }} />
          </div>
        </Modal>
      )}

      {removing && (
        <Modal
          title={t('settings.access.mcp.removeTitle')}
          hue={HUE}
          onClose={() => setRemoving(null)}
          footer={
            <>
              <span className="flex-1" />
              <Button
                kind="ghost"
                labelled
                icon={<IconClose />}
                title={t('common.cancel')}
                onClick={() => setRemoving(null)}
                disabled={busy}
              />
              <Button
                kind="ghost"
                icon={<IconTrash width={16} height={16} />}
                onClick={() => void doRemove(removing)}
                disabled={busy}
              >
                {t('settings.access.mcp.remove')}
              </Button>
            </>
          }
        >
          <p className="text-sm text-carbon-text">
            {t('settings.access.mcp.removeConfirm', { name: nameOf(clientById(removing.kind)) })}
          </p>
        </Modal>
      )}
    </>
  );
}

/**
 * AddClientWindow is the picker, the form for the picked client and, once its
 * token exists, that client's setup and the wait for its first call, all in
 * one window as GlimStone's pickers do.
 */
function AddClientWindow({
  base,
  waiting,
  onCreated,
  onClose,
}: {
  base: Omit<McpSnippetInput, 'token'>;
  /** The row of the client made here, as the card last read it. */
  waiting: McpClientRow | undefined;
  onCreated: (row: McpClientRow) => void;
  onClose: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [picked, setPicked] = useState<McpClient | null>(null);
  const [name, setName] = useState('');
  const [preset, setPreset] = useState<McpPreset>('addRead');
  const [creating, setCreating] = useState(false);
  const [shake, setShake] = useState(0);
  const [made, setMade] = useState<{ row: McpClientRow; secret: string } | null>(null);

  const nameOf = (c: McpClient) => (c === OTHER_CLIENT ? t('settings.access.mcp.other') : c.name);

  function pick(id: string) {
    const c = [...LOCAL_CLIENTS, ...CLOUD_CLIENTS, OTHER_CLIENT].find((x) => x.id === id) ?? OTHER_CLIENT;
    setPicked(c);
    setName(nameOf(c));
  }

  async function create() {
    if (!picked || name.trim() === '') return;
    setCreating(true);
    try {
      const res = await createMcpClient(picked.id, name.trim(), PRESET_SCOPES[preset]);
      setMade({ row: res.client, secret: res.secret });
      onCreated(res.client);
    } catch (e) {
      // The window stays open with the typed name; the reason goes to the toast.
      toast(t('settings.access.tokens.createFailed', { error: String(e).replace(/^Error:\s*/, '') }), 'fail');
      setShake((n) => n + 1);
    } finally {
      setCreating(false);
    }
  }

  const brand = (c: McpClient): Brand => ({
    id: c.id,
    name: nameOf(c),
    sub: c.kind ? t(KIND_LABEL[c.kind]) : undefined,
    tile: c.tile,
    mark: (
      <span className={c === OTHER_CLIENT ? 'glim-house-mark flex h-full w-full items-center justify-center' : 'contents'}>
        <ClientMark client={c} />
      </span>
    ),
  });
  const groups: { id: string; title: string; hint?: string; brands: Brand[] }[] = [
    { id: 'local', title: t('settings.access.mcp.groupLocal'), brands: LOCAL_CLIENTS.map(brand) },
    {
      id: 'cloud',
      title: t('settings.access.mcp.groupCloud'),
      hint: t('settings.access.mcp.cloudHint'),
      brands: CLOUD_CLIENTS.map(brand),
    },
    { id: 'other', title: '', brands: [brand(OTHER_CLIENT)] },
  ]
    .map((g) => ({ ...g, brands: g.brands.filter((b) => brandMatches(b, query)) }))
    .filter((g) => g.brands.length > 0);

  let title = t('settings.access.mcp.add');
  if (picked) title = t('settings.access.mcp.connectTitle', { name: nameOf(picked) });

  let footer: ReactNode;
  if (made) {
    footer = (
      <>
        <span className="flex-1" />
        <Button kind="primary" labelled icon={<IconCheck />} title={t('settings.access.tokens.done')} onClick={onClose} />
      </>
    );
  } else if (picked) {
    footer = (
      <>
        <Button
          kind="secondary"
          labelled
          icon={<IconBack className="rtl:-scale-x-100" />}
          title={t('onboarding.back')}
          onClick={() => {
            setPicked(null);
            setQuery('');
          }}
          disabled={creating}
        />
        <span className="flex-1" />
        <Button shake={shake} onClick={() => void create()} disabled={creating || name.trim() === ''}>
          {creating ? t('settings.access.mcp.connecting') : t('settings.access.mcp.connect')}
        </Button>
      </>
    );
  } else {
    footer = (
      <>
        <span className="flex-1" />
        <Button kind="secondary" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
      </>
    );
  }

  return (
    <Modal
      title={title}
      hint={picked ? undefined : t('settings.access.mcp.pickHint')}
      hue={HUE}
      wide
      height={picked ? 'content' : 'capped'}
      holdHeight={!picked && query !== ''}
      right={!picked && <PickerSearch value={query} onChange={setQuery} placeholder={t('settings.access.mcp.search')} />}
      onClose={() => (creating ? undefined : onClose())}
      footer={footer}
    >
      {!picked && (
        <div className="flex flex-col gap-4">
          {groups.length === 0 && (
            <p className="px-2 py-6 text-center text-sm text-carbon-textMuted">{t('settings.access.mcp.noMatch')}</p>
          )}
          {groups.map((g) => (
            <div key={g.id} className="flex flex-col gap-2">
              {g.title && <SubHead label={g.title} hint={g.hint} />}
              <BrandPicker brands={g.brands} none="" onPick={pick} />
            </div>
          ))}
        </div>
      )}

      {picked && !made && (
        <div className="flex flex-col gap-4">
          {picked.note && <p className="text-sm text-carbon-textSub">{t(picked.note)}</p>}
          <SettingRow control="long" label={t('settings.access.tokens.name')} hint={t('settings.access.mcp.nameHint')}>
            <TextInput
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void create();
              }}
            />
          </SettingRow>
          <SettingRow label={t('settings.access.tokens.rights')} hint={t('settings.access.mcp.rightsHint')}>
            <Tabs
              variant="well"
              size="sm"
              inline
              hug
              label={t('settings.access.tokens.rights')}
              active={preset}
              onSelect={(id) => setPreset(id as McpPreset)}
              items={(Object.keys(PRESET_SCOPES) as McpPreset[]).map((id) => ({ id, label: t(MCP_PRESET_LABEL[id]) }))}
            />
          </SettingRow>
        </div>
      )}

      {picked && made && (
        <div className="flex flex-col gap-4">
          <TokenField secret={made.secret} />
          <ClientSetup client={picked} input={{ ...base, token: made.secret }} />
          <FirstCall client={picked} row={waiting} />
        </div>
      )}
    </Modal>
  );
}

/** TokenField shows a new token once, with its copy button. */
function TokenField({ secret }: { secret: string }) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const [copies, setCopies] = useState(0);
  return (
    <div className="flex flex-col gap-1.5">
      <span className="flex items-center gap-1.5 text-sm text-carbon-text">
        {t('settings.access.mcp.tokenLabel')}
        <InfoBubble tip={t('settings.access.mcp.tokenHint')} />
      </span>
      <div className="flex items-center gap-2">
        <div className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
          <code className="glim-num block overflow-x-auto whitespace-nowrap text-xs text-carbon-text" dir="ltr">
            {secret}
          </code>
        </div>
        <IconBadge
          labelled
          hue={HUE}
          icon={copied ? <IconCheckDrawn width={16} height={16} /> : <IconCopy width={16} height={16} />}
          title={copied ? t('settings.access.tokens.copied') : t('settings.access.tokens.copy')}
          aria-label={copied ? t('settings.access.tokens.copied') : t('settings.access.tokens.copy')}
          confirm={copies}
          onClick={async () => {
            if (await copyToClipboard(secret)) {
              setCopied(true);
              setCopies((n) => n + 1);
              setTimeout(() => setCopied(false), 1800);
            }
          }}
        />
      </div>
    </div>
  );
}

/**
 * ClientSetup is what one client needs: its configuration to copy, where that
 * goes, and how the client comes by the token without holding it where it can.
 */
function ClientSetup({ client, input }: { client: McpClient; input: McpSnippetInput }) {
  const { t } = useT();
  const [copies, setCopies] = useState(0);
  const app = client === OTHER_CLIENT ? t('settings.access.mcp.other') : client.name;
  const snippet = snippetFor(client, input);
  const setup = client.setup;

  let intro = t('settings.access.mcp.setupOther');
  if (setup.kind === 'command') intro = t('settings.access.mcp.setupCommand');
  if (setup.kind === 'file') intro = t('settings.access.mcp.setupFile', { app });
  if (setup.kind === 'form') intro = t('settings.access.mcp.setupForm', { app, ui: setup.ui });

  return (
    <div className="flex flex-col gap-2.5">
      {client.group === 'cloud' && <p className="text-sm text-statusWarn">{t('settings.access.mcp.cloudHint')}</p>}
      <SubHead
        label={t('settings.access.mcp.config')}
        right={
          <IconBadge
            labelled
            hue={HUE}
            icon={<IconCopy width={16} height={16} />}
            title={t('settings.access.tokens.copy')}
            aria-label={t('settings.access.tokens.copy')}
            confirm={copies}
            onClick={async () => {
              if (await copyToClipboard(snippet)) setCopies((n) => n + 1);
            }}
          />
        }
      />
      <p className="text-sm text-carbon-textSub">
        {intro}
        {client.node && ` ${t('settings.access.mcp.setupNeedsNode', { app })}`}
      </p>
      <pre
        dir="ltr"
        className="overflow-x-auto whitespace-pre-wrap rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-start font-mono text-xs leading-relaxed text-carbon-text [overflow-wrap:anywhere]"
      >
        <code>{snippet}</code>
      </pre>
      {setup.kind === 'file' && (
        <div className="flex flex-col gap-1 pt-1">
          <span className="text-xs text-carbon-textSub">{t('settings.access.mcp.where')}</span>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
            {OS_PATHS.filter(([os]) => setup.paths[os]).map(([os, label]) => (
              <div key={os} className="contents">
                <dt className="text-carbon-textMuted">{label}</dt>
                <dd dir="ltr" className="min-w-0 break-all text-start font-mono text-carbon-text">
                  {setup.paths[os]}
                </dd>
              </div>
            ))}
          </dl>
          {setup.ui && <p className="text-sm text-carbon-textSub">{t('settings.access.mcp.setupFileUi', { app, ui: setup.ui })}</p>}
          {setup.paste && (
            <p className="text-sm text-carbon-textSub">{t('settings.access.mcp.setupPaste', { app, ui: setup.paste })}</p>
          )}
        </div>
      )}
      <TokenLine client={client} app={app} />
    </div>
  );
}

function TokenLine({ client, app }: { client: McpClient; app: string }) {
  const { t } = useT();
  const way = client.token;
  let line: string;
  let tip: string | undefined;
  switch (way.kind) {
    case 'tokenFile':
      line = t('settings.access.mcp.tokenFile', { app });
      if (client.setup.kind === 'file') tip = t('settings.access.mcp.tokenFileTip', { app });
      break;
    case 'env':
      line = t('settings.access.mcp.tokenEnv', { app, var: TOKEN_VARIABLE });
      if (way.unsure) line += ` ${t('settings.access.mcp.tokenEnvUnsure', { app })}`;
      tip = t('settings.access.mcp.tokenEnvTip', { app });
      break;
    case 'prompt':
      line = t('settings.access.mcp.tokenPrompt', { app });
      break;
    case 'dotenv':
      line = t('settings.access.mcp.tokenDotenv', { app, var: TOKEN_VARIABLE, file: way.file });
      break;
    case 'inFile':
      line = t('settings.access.mcp.tokenInFile', { app });
      break;
    case 'inApp':
      line = t('settings.access.mcp.tokenInApp', { app });
      break;
    case 'credential':
      line = t('settings.access.mcp.tokenCredential', { app });
      break;
    case 'vendor':
      line = t('settings.access.mcp.tokenVendor', { app });
      break;
    default:
      line = t('settings.access.mcp.tokenOther');
  }
  return (
    <p className="pt-1 text-sm text-carbon-textSub">
      {line}
      {tip && (
        <span className="ms-1 inline-flex align-[-0.125rem]">
          <InfoBubble tip={tip} />
        </span>
      )}
    </p>
  );
}

/** FirstCall waits for the new client's first call, which the card polls for. */
function FirstCall({ client, row }: { client: McpClient; row: McpClientRow | undefined }) {
  const { t } = useT();
  const app = client === OTHER_CLIENT ? t('settings.access.mcp.other') : client.name;
  if (row?.lastCall) {
    return (
      <p role="status" className="flex items-start gap-2 text-sm text-statusOk">
        <span className="mt-1.5 inline-block h-2 w-2 shrink-0 rounded-full bg-statusOk" />
        <span>
          <strong className="font-semibold">{t('settings.access.mcp.connected')}</strong>{' '}
          {t('settings.access.mcp.connectedAt', { app, time: fmtDate(row.lastCall) })}
        </span>
      </p>
    );
  }
  return (
    <p role="status" className="flex items-start gap-2 text-sm text-carbon-text">
      <span className="mt-1.5 inline-block h-2 w-2 shrink-0 rounded-full bg-accent" />
      <span>
        <strong className="font-semibold">{t('settings.access.mcp.waiting')}</strong>{' '}
        <span className="text-carbon-textSub">{t('settings.access.mcp.waitingHint', { app })}</span>
      </span>
    </p>
  );
}
