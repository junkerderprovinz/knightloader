import { useEffect, useState } from 'react';
import { Button, Card, Field, InfoBubble, Modal, SectionTitle, TextInput } from '../../components/ui';
import { QRCode } from '../../components/QRCode';
import {
  type ApiToken,
  type AuthState,
  type NewApiToken,
  type PairingCode,
  type RemoteAccessInfo,
  createToken,
  fetchAuth,
  fetchRemoteAccess,
  fetchTokens,
  generatePairingCode,
  revokeToken,
  setPassword,
} from '../../lib/api';
import { fmtDate } from '../../lib/format';
import { useInstallPrompt } from '../../lib/pwaInstall';
import { useT, type TranslationKey } from '../../lib/i18n';
import { IconKey, IconPlus, IconTrash, IconWarning } from '../../lib/icons';
import { useFeatures } from './context';
import { label, useTx } from './tx';

/**
 * The Access page: the password lock (unchanged from before this wave), the
 * intake ports table (unchanged), and what build-plan.md section 8's Wave
 * 11 amendment on 11C adds - named API tokens and the Remote access story
 * (reachable addresses, a QR code, the PWA install BrowserTools.tsx also
 * offers, and the loud exposure warning).
 *
 * The strings the two new sections need are not in en.ts yet - locale files
 * are one writer's lane per wave (11G, phase 3 of this one), the same
 * arrangement Help.tsx, Diagnostics.tsx and BrowserTools.tsx already use, so
 * the lookup below asks the real catalogue first and falls back to English
 * here.
 */
const PENDING = {
  'settings.access.tokens.title': 'API tokens',
  'settings.access.tokens.intro':
    'Named credentials for a script, a browser extension or a phone. Each one can be revoked on its own, without changing the shared password every other client uses.',
  'settings.access.tokens.empty': 'No tokens issued yet.',
  'settings.access.tokens.new': 'New token',
  'settings.access.tokens.namePlaceholder': 'e.g. my phone',
  'settings.access.tokens.cancel': 'Cancel',
  'settings.access.tokens.create': 'Create',
  'settings.access.tokens.creating': 'Creating…',
  'settings.access.tokens.created': 'Created',
  'settings.access.tokens.lastUsed': 'Last used',
  'settings.access.tokens.neverUsed': 'never',
  'settings.access.tokens.revoke': 'Revoke',
  'settings.access.tokens.secretTitle': 'Copy this token now',
  'settings.access.tokens.secretWarning':
    'This is the only time this token is shown. It is stored as a one-way hash on this instance, so if it is lost there is no way to read it back, only to revoke it and create a new one.',
  'settings.access.tokens.copy': 'Copy',
  'settings.access.tokens.copied': 'Copied',
  'settings.access.tokens.done': 'Done',
  'settings.access.tokens.howToUse': 'Send it as a header: Authorization: Bearer <token>',
  'settings.access.tokens.createFailed': 'Could not create the token: {error}',

  'settings.access.remote.title': 'Remote access',
  'settings.access.remote.desktopNote':
    'This is the desktop build. It does not serve the API over the network at all, so there is nothing here to reach from outside this application.',
  'settings.access.remote.exposedWarning':
    'This instance just answered a request from outside this machine, and no password protects it. Anyone who can reach it can see and control every download. Set a password above now.',
  'settings.access.remote.noRelayBody':
    'There is no account service and no pairing step, and there never will be: running one would mean an ongoing hosted service with real cost and liability, not a feature of a self-hosted binary. Reaching this instance from outside your own network is your own port forward, reverse proxy or VPN, the same as any other self-hosted server.',
  'settings.access.remote.addressesTitle': 'Addresses this instance answers on',
  'settings.access.remote.noAddresses': 'No address could be determined for this request.',
  'settings.access.remote.loopback': 'this machine only',
  'settings.access.remote.scanHint': 'Only works on the same network as this instance.',
  'settings.access.remote.installTitle': 'Install as an app',
  'settings.access.remote.installBody':
    'Add KnightLoader to a home screen or app list for a faster launch, without the browser chrome.',
  'settings.access.remote.install': 'Install',
  'settings.access.remote.installIOS':
    'On iPhone or iPad: open this page in Safari, tap Share, then "Add to Home Screen".',
  'settings.access.remote.pairTitle': 'Pair another instance',
  'settings.access.remote.pairBody':
    "Add a KnightLoader you already run - no address to type, no account, nothing hosted. Generate a code here, then paste it into that instance's own Instances page.",
  'settings.access.remote.pairGenerate': 'Generate pairing code',
  'settings.access.remote.pairExpires': 'Valid for {min} minutes, then it expires unused.',

  'settings.access.intakePortsHint':
    'Other ways this instance can be reached directly, outside the normal login - each with its own reachability shown here.',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return (key: PendingKey, vars?: Record<string, string | number>) => {
    let s = (t(key as unknown as TranslationKey) as string | undefined) ?? PENDING[key];
    if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
    return s;
  };
}

export function Access() {
  const { tx } = useTx();
  const cx = useCx();
  const { features } = useFeatures();

  // The listeners this instance answers on are a property of the build and the
  // environment, not of settings.json, so they are read out of the module
  // registry rather than described a second time here.
  const listeners = features.modules.filter((m) => m.page === 'access');

  return (
    <div className="flex flex-col gap-10">
      <PasswordCard />

      <RemoteAccessSection cx={cx} />
      <TokensSection cx={cx} />

      {listeners.length > 0 && (
          <Card className="flex flex-col gap-4">
            <SectionTitle hue={4} hint={cx('settings.access.intakePortsHint')}>
              {tx('settings.sectionIntakePorts')}
            </SectionTitle>
            {listeners.map((m) => (
              <div key={m.id} className="flex items-baseline gap-3">
                <span className="flex items-center text-sm text-carbon-text">
                  {label(tx, 'settings.module.', m.id)}
                  {m.reason && <InfoBubble tip={m.reason} />}
                </span>
                <span className="flex-1" />
                <span className="text-[11px] text-carbon-textMuted" dir="ltr">
                  {m.detail || tx(m.enabled ? 'settings.modules.on' : 'settings.modules.off')}
                </span>
              </div>
            ))}
          </Card>
      )}
    </div>
  );
}

// PasswordCard owns the password lock. It saves on its own button rather than
// with the rest of the settings: a password is not a preference you change by
// accident while adjusting the speed limit, and it does not go through
// PUT /api/settings at all.
function PasswordCard() {
  const { t } = useT();
  const [auth, setAuth] = useState<AuthState | null>(null);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    fetchAuth()
      .then(setAuth)
      .catch(() => setAuth(null));
  }, []);

  async function onApply() {
    setError('');
    try {
      setAuth(await setPassword(current, next));
      setCurrent('');
      setNext('');
      setDone(true);
      setTimeout(() => setDone(false), 1800);
    } catch (e) {
      setError(String(e).replace(/^Error:\s*/, ''));
    }
  }

  const locked = auth?.enabled ?? false;

  return (
      <Card className="flex flex-col gap-5">
        <SectionTitle hue={0}>{t('auth.password')}</SectionTitle>
        <p className={`text-sm ${locked ? 'text-statusOk' : 'text-carbon-textSub'}`}>
          {locked ? t('settings.lockOn') : t('settings.lockOff')}
        </p>
        {locked && (
          <Field label={t('settings.passwordCurrent')}>
            <TextInput type="password" value={current} onChange={(e) => setCurrent(e.target.value)} />
          </Field>
        )}
        <Field label={t('settings.passwordNew')} hint={t('settings.passwordHint')}>
          <TextInput type="password" value={next} onChange={(e) => setNext(e.target.value)} />
        </Field>
        <div className="flex items-center gap-3">
          <Button kind="secondary" onClick={onApply} disabled={locked ? current === '' : next === ''}>
            {next === '' && locked ? t('settings.removePassword') : t('settings.setPassword')}
          </Button>
          {done && <span className="text-statusOk text-sm">{t('settings.passwordSaved')}</span>}
          {error && <span className="text-statusFail text-sm">{error}</span>}
        </div>
      </Card>
  );
}

// ---- Remote access ---------------------------------------------------------

// RemoteAccessSection reads GET /api/remote-access once on mount. Nothing
// here streams: the addresses and the exposure flag are a property of how
// the process is deployed, not of anything that changes while this page is
// open, so a poll would only cost requests for no benefit - reopening the
// page is what a person actually does after changing KL_ADDR or a reverse
// proxy rule.
function RemoteAccessSection({ cx }: { cx: (k: PendingKey) => string }) {
  const [info, setInfo] = useState<RemoteAccessInfo | null>(null);
  const { available: canInstall, promptInstall } = useInstallPrompt();

  useEffect(() => {
    fetchRemoteAccess()
      .then(setInfo)
      .catch(() => setInfo(null));
  }, []);

  if (!info) return null;

  if (info.deployment === 'desktop') {
    return (
        <Card>
          <SectionTitle hue={1}>{cx('settings.access.remote.title')}</SectionTitle>
          <p className="text-sm text-carbon-textSub">{cx('settings.access.remote.desktopNote')}</p>
        </Card>
    );
  }

  // Safari (desktop and iOS) never fires beforeinstallprompt, so
  // useInstallPrompt's `available` is permanently false there - this is the
  // one place that still has something useful to say instead of nothing:
  // the manual Share-sheet route, iOS's only way to install any web app.
  const iOS = /iphone|ipad|ipod/i.test(navigator.userAgent);
  const primary = info.addresses[0];

  return (
    <>
      {info.exposed && (
        <div className="flex items-start gap-3 rounded-[var(--radius-card)] bg-statusFailBg p-4">
          <IconWarning width={20} height={20} className="mt-0.5 shrink-0 text-statusFail" />
          <p className="text-sm font-medium text-statusFail">{cx('settings.access.remote.exposedWarning')}</p>
        </div>
      )}

      <Card className="flex flex-col gap-4 sm:flex-row">
        <SectionTitle hue={1}>{cx('settings.access.remote.title')}</SectionTitle>
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <p className="text-[11px] text-carbon-textMuted">{cx('settings.access.remote.noRelayBody')}</p>
          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-semibold text-carbon-textSub">
              {cx('settings.access.remote.addressesTitle')}
            </span>
            {info.addresses.length === 0 && (
              <span className="text-[11px] text-carbon-textMuted">{cx('settings.access.remote.noAddresses')}</span>
            )}
            {info.addresses.map((a) => (
              <div key={a.url} className="flex items-center gap-2 text-sm">
                <span className="glim-num min-w-0 flex-1 truncate text-carbon-text" dir="ltr">
                  {a.url}
                </span>
                {a.loopback && (
                  <span className="shrink-0 text-[11px] text-carbon-textMuted">
                    {cx('settings.access.remote.loopback')}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
        {info.qr && primary && (
          <div className="flex shrink-0 flex-col items-center gap-2 self-start">
            <QRCode matrix={info.qr} label={primary.url} size={144} />
            <span className="max-w-[144px] text-center text-[11px] text-carbon-textMuted">
              {cx('settings.access.remote.scanHint')}
            </span>
          </div>
        )}
      </Card>

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={2}>{cx('settings.access.remote.installTitle')}</SectionTitle>
        <p className="text-[11px] text-carbon-textMuted">{cx('settings.access.remote.installBody')}</p>
        {canInstall && (
          <div>
            <Button kind="secondary" onClick={() => void promptInstall()}>
              {cx('settings.access.remote.install')}
            </Button>
          </div>
        )}
        {!canInstall && iOS && (
          <p className="text-[11px] text-carbon-textMuted">{cx('settings.access.remote.installIOS')}</p>
        )}
      </Card>

      <PairingCard cx={cx} />
    </>
  );
}

// PairingCard is the OTHER half of Instances.tsx's own "Pair with a code"
// card: this instance generates the code (internal/api/routes_pairing.go's
// POST /api/instances/pairing-code), the other instance's Instances page
// redeems it. No account, no vendor relay - see noRelayBody just above,
// still true: this only ever reaches a KnightLoader you already run and
// already have network access to, the same as typing its address by hand
// would.
function PairingCard({ cx }: { cx: (k: PendingKey, vars?: Record<string, string | number>) => string }) {
  const [code, setCode] = useState<PairingCode | null>(null);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const [err, setErr] = useState('');

  async function onGenerate() {
    setErr('');
    setBusy(true);
    try {
      setCode(await generatePairingCode());
      setCopied(false);
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="flex flex-col gap-3">
      <SectionTitle hue={3}>{cx('settings.access.remote.pairTitle')}</SectionTitle>
      <p className="text-[11px] text-carbon-textMuted">{cx('settings.access.remote.pairBody')}</p>
      {!code && (
        <div>
          <Button kind="secondary" onClick={() => void onGenerate()} disabled={busy}>
            {cx('settings.access.remote.pairGenerate')}
          </Button>
        </div>
      )}
      {code && (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
            <code className="glim-num min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-xs text-carbon-text" dir="ltr">
              {code.code}
            </code>
            {'clipboard' in navigator && (
              <Button
                kind="ghost"
                className="shrink-0 px-2.5 text-xs"
                onClick={async () => {
                  await navigator.clipboard.writeText(code.code);
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1800);
                }}
              >
                {copied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
              </Button>
            )}
          </div>
          <span className="text-[11px] text-carbon-textMuted">
            {cx('settings.access.remote.pairExpires', { min: Math.round(code.expiresIn / 60) })}
          </span>
        </div>
      )}
      {err && <p className="text-sm text-statusFail">{err}</p>}
    </Card>
  );
}

// ---- API tokens -------------------------------------------------------------

function TokensSection({ cx }: { cx: (k: PendingKey) => string }) {
  const [tokens, setTokens] = useState<ApiToken[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');
  const [created, setCreated] = useState<NewApiToken | null>(null);
  const [copied, setCopied] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);

  const load = () => fetchTokens().then(setTokens).catch(() => {});
  useEffect(() => {
    load();
  }, []);

  async function onCreate() {
    setCreateError('');
    setCreating(true);
    try {
      const tok = await createToken(name.trim());
      setCreated(tok);
      setName('');
      await load();
    } catch (e) {
      setCreateError(cx('settings.access.tokens.createFailed').replace('{error}', String(e).replace(/^Error:\s*/, '')));
    } finally {
      setCreating(false);
    }
  }

  async function onRevoke(id: string) {
    setRevoking(id);
    try {
      await revokeToken(id);
      await load();
    } finally {
      setRevoking(null);
    }
  }

  function closeCreate() {
    setShowCreate(false);
    setCreated(null);
    setCreateError('');
    setCopied(false);
  }

  return (
    <>
      <Card className="flex flex-col gap-3">
        <SectionTitle
          hue={3}
          right={
            <Button
              kind="secondary"
              className="px-2.5 text-xs"
              icon={<IconPlus width={14} height={14} />}
              onClick={() => setShowCreate(true)}
            >
              {cx('settings.access.tokens.new')}
            </Button>
          }
        >
          {cx('settings.access.tokens.title')}
        </SectionTitle>
        <p className="text-[11px] text-carbon-textMuted">{cx('settings.access.tokens.intro')}</p>
        {tokens.length === 0 ? (
          <p className="text-sm text-carbon-textMuted">{cx('settings.access.tokens.empty')}</p>
        ) : (
          <div className="flex flex-col divide-y divide-carbon-border/40">
            {tokens.map((tok) => (
              <div key={tok.id} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
                <IconKey width={16} height={16} className="shrink-0 text-carbon-textMuted" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm text-carbon-text">{tok.name}</div>
                  <div className="text-[11px] text-carbon-textMuted">
                    {cx('settings.access.tokens.created')} {fmtDate(tok.createdAt)}
                    {' · '}
                    {cx('settings.access.tokens.lastUsed')}{' '}
                    {tok.lastUsed ? fmtDate(tok.lastUsed) : cx('settings.access.tokens.neverUsed')}
                  </div>
                </div>
                <Button
                  kind="danger"
                  className="shrink-0 px-2.5 text-xs"
                  icon={<IconTrash width={15} height={15} />}
                  disabled={revoking === tok.id}
                  onClick={() => void onRevoke(tok.id)}
                >
                  {cx('settings.access.tokens.revoke')}
                </Button>
              </div>
            ))}
          </div>
        )}
      </Card>

      {showCreate && !created && (
        <Modal
          title={cx('settings.access.tokens.new')}
          onClose={() => (creating ? undefined : closeCreate())}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="ghost" onClick={closeCreate} disabled={creating}>
                {cx('settings.access.tokens.cancel')}
              </Button>
              <Button kind="primary" onClick={() => void onCreate()} disabled={creating || name.trim() === ''}>
                {creating ? cx('settings.access.tokens.creating') : cx('settings.access.tokens.create')}
              </Button>
            </>
          }
        >
          <Field label={cx('settings.access.tokens.title')}>
            <TextInput
              autoFocus
              placeholder={cx('settings.access.tokens.namePlaceholder')}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && name.trim() !== '' && !creating) void onCreate();
              }}
            />
          </Field>
          {createError && <p className="mt-2 text-sm text-statusFail">{createError}</p>}
        </Modal>
      )}

      {created && (
        <Modal
          title={cx('settings.access.tokens.secretTitle')}
          onClose={closeCreate}
          footer={
            <>
              <span className="flex-1" />
              <Button kind="primary" onClick={closeCreate}>
                {cx('settings.access.tokens.done')}
              </Button>
            </>
          }
        >
          <div className="flex flex-col gap-3">
            <p className="text-sm text-statusFail">{cx('settings.access.tokens.secretWarning')}</p>
            <div className="flex items-center gap-2 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
              <code className="glim-num min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-xs text-carbon-text" dir="ltr">
                {created.secret}
              </code>
              {'clipboard' in navigator && (
                <Button
                  kind="ghost"
                  className="shrink-0 px-2.5 text-xs"
                  onClick={async () => {
                    await navigator.clipboard.writeText(created.secret);
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1800);
                  }}
                >
                  {copied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                </Button>
              )}
            </div>
            <p className="text-[11px] text-carbon-textMuted" dir="ltr">
              {cx('settings.access.tokens.howToUse')}
            </p>
          </div>
        </Modal>
      )}
    </>
  );
}
