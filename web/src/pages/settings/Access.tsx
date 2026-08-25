import { useEffect, useState } from 'react';
import { Button, Card, Field, IconBadge, InfoBubble, Modal, SectionTitle, TextArea, TextInput } from '../../components/ui';
import { QRCode } from '../../components/QRCode';
import {
  type ApiToken,
  type AuthState,
  type Instance,
  type NewApiToken,
  type PairingCode,
  type RelayConfig,
  type RemoteAccessInfo,
  createToken,
  fetchAuth,
  fetchInstances,
  fetchRelayConfig,
  fetchRemoteAccess,
  fetchTokens,
  generatePairingCode,
  revokeToken,
  saveRelayConfig,
  setPassword,
} from '../../lib/api';
import { copyToClipboard } from '../../lib/clipboard';
import { fmtDate } from '../../lib/format';
import { useInstallPrompt } from '../../lib/pwaInstall';
import { useT, type TranslationKey } from '../../lib/i18n';
import { IconCheck, IconClipboard, IconKey, IconPlus, IconTrash, IconWarning } from '../../lib/icons';
import { useToast } from '../../lib/toast';
import { useDraft, useFeatures } from './context';
import { NeutralSwitch } from './controls';
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
  'settings.access.remote.vsPairing':
    "This is for opening this instance's own interface on another device by hand (a phone, another browser). Pairing another KnightLoader you run yourself, so the two show up on each other's Instances page, is the separate card below.",
  'settings.access.remote.addressesTitle': 'Addresses this instance answers on',
  'settings.access.remote.noAddresses': 'No address could be determined for this request.',
  'settings.access.remote.loopback': 'this machine only',
  'settings.access.remote.domain': 'domain',
  'settings.access.remote.scanHint': 'Only works on the same network as this instance.',

  'settings.access.identity.title': "This instance's identity",
  'settings.access.identity.nameLabel': 'Name',
  'settings.access.identity.namePlaceholder': 'e.g. Home server',
  'settings.access.identity.nameHint':
    'Offered first when pairing and in the QR code below, instead of whatever the OS or container runtime happens to call this machine. Optional - leave it empty to keep using that.',
  'settings.access.identity.domainsLabel': 'Known domains',
  'settings.access.identity.domainsHint':
    'Remembered automatically the first time a request actually arrives on one, so it stays listed here even when later requests come in over the LAN IP instead. Add one by hand for a domain that is already configured but has not been visited through yet - one full address per line, e.g. https://kl.example.com.',

  'settings.access.remote.installTitle': 'Install as an app',
  'settings.access.remote.installBody':
    'Add KnightLoader to a home screen or app list for a faster launch, without the browser chrome.',
  'settings.access.remote.install': 'Install',
  'settings.access.remote.installIOS':
    'On iPhone or iPad: open this page in Safari, tap Share, then "Add to Home Screen".',
  'settings.access.remote.pairTitle': 'Pair another instance',
  'settings.access.remote.pairBody':
    "Add a KnightLoader you already run - no address to type, no account, nothing hosted. Generate a code here, then paste it into that instance's own Instances page.",
  'settings.access.remote.pairPrereq':
    'For this to work from outside your own network - say, pairing with a phone away from home - this instance needs a domain behind a reverse proxy, or a VPN, set up first. A LAN-only address only pairs with another instance on the same network.',
  'settings.access.remote.pairGenerate': 'Generate pairing code',
  'settings.access.remote.pairExpires': 'Valid for {min} minutes, then it expires unused.',
  'settings.access.remote.pairWhere': 'Not here - on the other instance, under Settings → Instances.',
  'settings.access.remote.pairScan': 'Scan the QR code with the KnightLoader app.',

  'settings.access.relay.title': 'Relay',
  'settings.access.relay.body':
    'A relay is a small separate server that instances dial out to, so two KnightLoaders which cannot reach each other directly - each behind its own NAT, on different networks - still find each other. Every instance you give the same address and key to appears on the Instances page of the others by itself, with no pairing code to carry across.',
  'settings.access.relay.selfHosted':
    'Nobody runs a relay for you. It is a separate binary you host yourself, the same way you already host KnightLoader, and it only routes messages between your own instances: no download and no file byte ever travels over it. Leaving this empty changes nothing about the rest of this page.',
  'settings.access.relay.urlLabel': 'Relay address',
  'settings.access.relay.urlPlaceholder': 'https://relay.example.com',
  'settings.access.relay.urlHint':
    'The address your own relay answers on, behind TLS for anything beyond a LAN test. Clearing it disconnects from the relay and leaves every other way of reaching this instance untouched.',
  'settings.access.relay.keyLabel': 'Relay key',
  'settings.access.relay.keyHint':
    'One shared secret is the whole of the authorisation, so every instance that should see the others gets the same one. It is stored encrypted here, like a debrid key, and is never shown again once saved - if it is lost, make a new one and enter that everywhere.',
  'settings.access.relay.keyPlaceholderSet': 'Stored. Type a new key to replace it.',
  'settings.access.relay.keyPlaceholderUnset': 'Paste the relay key',
  'settings.access.relay.keySet': 'Key stored',
  'settings.access.relay.keyUnset': 'No key stored',
  'settings.access.relay.keyClear': 'Remove the stored key',
  'settings.access.relay.save': 'Save',
  'settings.access.relay.saving': 'Saving…',
  'settings.access.relay.saved': 'Saved',
  'settings.access.relay.siblingsTitle': 'Visible through the relay',
  'settings.access.relay.siblingsOff': 'Enter an address and a key to see the instances that share them.',
  'settings.access.relay.siblingsEmpty':
    'Nothing right now. Either no other instance is connected with this key, or the relay cannot be reached from here. Both look the same from this side, and neither stops this instance doing anything else.',

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
  const { features, toggle } = useFeatures();
  const { toast } = useToast();
  const [busyId, setBusyId] = useState<string | null>(null);

  // The listeners this instance answers on are a property of the build and the
  // environment, not of settings.json, so they are read out of the module
  // registry rather than described a second time here.
  const listeners = features.modules.filter((m) => m.page === 'access');

  // The same switch Modules.tsx's own row runs, reached from a second place
  // on purpose (jdp, 2026-08-24: "im Zugang tab ist bei CnL kein Toggle ...
  // für was brauchen wir das denn eigentlich" - the toggle belongs on the
  // page where the port itself lives, not only on the module registry's own
  // overview). Same failure handling as that row: the server refuses a
  // switch it cannot honour and says why, shown rather than swallowed.
  async function onToggle(id: string, next: boolean) {
    setBusyId(id);
    try {
      await toggle(id, next);
    } catch (e) {
      toast(tx('settings.modules.switchFailed', { reason: String(e).replace(/^Error:\s*/, '') }), 'fail');
    } finally {
      setBusyId(null);
    }
  }

  return (
    <div className="flex flex-col gap-10">
      <PasswordCard />

      <RemoteAccessSection cx={cx} />
      {/* Outside RemoteAccessSection on purpose, even though it reads as the
          card right after that section's own PairingCard: that section
          renders nothing but a note on the desktop build, and the desktop
          build is the one that needs a relay MOST - it opens no port at all,
          so a relay is its only way to be paired with anything. */}
      <RelayCard cx={cx} />
      <TokensSection cx={cx} />

      {listeners.length > 0 && (
          <Card className="flex flex-col gap-4">
            <SectionTitle hue={7} hint={cx('settings.access.intakePortsHint')}>
              {tx('settings.sectionIntakePorts')}
            </SectionTitle>
            {listeners.map((m) => {
              const switchable = m.verdict === 'shipped' && m.switch !== 'none';
              return (
                <div key={m.id} className="flex items-baseline gap-3">
                  <span className="flex items-center text-sm text-carbon-text">
                    {label(tx, 'settings.module.', m.id)}
                    {m.reason && <InfoBubble tip={m.reason} />}
                  </span>
                  <span className="flex-1" />
                  <span className="text-[11px] text-carbon-textMuted" dir="ltr">
                    {m.detail || tx(m.enabled ? 'settings.modules.on' : 'settings.modules.off')}
                  </span>
                  {switchable && (
                    <NeutralSwitch
                      on={m.enabled}
                      disabled={busyId === m.id}
                      name={label(tx, 'settings.module.', m.id)}
                      onChange={(next) => void onToggle(m.id, next)}
                    />
                  )}
                </div>
              );
            })}
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
          <SectionTitle hue={1} hint={cx('settings.access.remote.desktopNote')}>
            {cx('settings.access.remote.title')}
          </SectionTitle>
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
        <SectionTitle
          hue={1}
          hint={`${cx('settings.access.remote.noRelayBody')} ${cx('settings.access.remote.vsPairing')}`}
        >
          {cx('settings.access.remote.title')}
        </SectionTitle>
        <div className="flex min-w-0 flex-1 flex-col gap-3">
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
                {a.domain && (
                  <span className="shrink-0 rounded-full bg-carbon-surface2 px-2 py-0.5 text-[11px] text-carbon-textSub">
                    {cx('settings.access.remote.domain')}
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

      <IdentityCard cx={cx} />

      <Card className="flex flex-col gap-3">
        <SectionTitle hue={3} hint={cx('settings.access.remote.installBody')}>
          {cx('settings.access.remote.installTitle')}
        </SectionTitle>
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

// IdentityCard: an optional friendly name and the domains this instance is
// known to be reachable through, both read/written through the normal
// settings draft (useDraft) and the shared Save bar, same as any other field
// on this page - not a live-preview control like Look.tsx's, so there is no
// reason to save it any differently.
//
// KnownDomains is shown here for the SAME reason a bare LAN IP still shows in
// the addresses card above: this is a normal settings field, editable
// independently of whatever request happened to load this page. The list
// itself is otherwise populated automatically (routes_remote.go's own
// rememberDomain, the moment a request actually arrives on a domain) - this
// textarea only matters for a domain that is already configured but has not
// been visited through yet, so pairingSelf and the QR code above have
// something to prefer before that first visit happens.
function IdentityCard({ cx }: { cx: (k: PendingKey) => string }) {
  const { cfg, patch } = useDraft();

  return (
    <Card className="flex flex-col gap-5">
      <SectionTitle hue={2}>{cx('settings.access.identity.title')}</SectionTitle>
      <Field label={cx('settings.access.identity.nameLabel')} hint={cx('settings.access.identity.nameHint')}>
        <TextInput
          placeholder={cx('settings.access.identity.namePlaceholder')}
          value={cfg.instanceName}
          onChange={(e) => patch({ instanceName: e.target.value })}
        />
      </Field>
      <Field label={cx('settings.access.identity.domainsLabel')} hint={cx('settings.access.identity.domainsHint')}>
        <TextArea
          rows={3}
          spellCheck={false}
          dir="ltr"
          value={(cfg.knownDomains ?? []).join('\n')}
          onChange={(e) => patch({ knownDomains: e.target.value.split('\n').filter((d) => d.trim() !== '') })}
        />
      </Field>
    </Card>
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
      <SectionTitle
        hue={4}
        hint={`${cx('settings.access.remote.pairBody')} ${cx('settings.access.remote.pairPrereq')}`}
        right={
          !code ? (
            <Button
              kind="secondary"
              hue={4}
              className="px-2.5 text-xs"
              icon={<IconPlus width={14} height={14} />}
              onClick={() => void onGenerate()}
              disabled={busy}
            >
              {cx('settings.access.remote.pairGenerate')}
            </Button>
          ) : undefined
        }
      >
        {cx('settings.access.remote.pairTitle')}
      </SectionTitle>
      <div className="flex flex-col gap-3 sm:flex-row">
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          {code && (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <div className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
                  <code className="glim-num block overflow-x-auto whitespace-nowrap text-xs text-carbon-text" dir="ltr">
                    {code.code}
                  </code>
                </div>
                <IconBadge
                  hue={4}
                  icon={copied ? <IconCheck width={14} height={14} /> : <IconClipboard width={14} height={14} />}
                  title={copied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                  aria-label={copied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                  onClick={async () => {
                    if (await copyToClipboard(code.code)) {
                      setCopied(true);
                      setTimeout(() => setCopied(false), 1800);
                    }
                  }}
                />
              </div>
              <p className="text-[11px] text-carbon-textMuted">
                {cx('settings.access.remote.pairExpires', { min: Math.round(code.expiresIn / 60) })}
              </p>
            </div>
          )}
          {err && <p className="text-sm text-statusFail">{err}</p>}
        </div>
        {code?.qr && (
          <div className="flex shrink-0 flex-col items-center gap-2 self-start">
            <QRCode matrix={code.qr} label={code.code} size={144} />
            <span className="max-w-[144px] text-center text-[11px] text-carbon-textMuted">
              {cx('settings.access.remote.pairScan')}
            </span>
          </div>
        )}
      </div>
    </Card>
  );
}

// ---- Relay -------------------------------------------------------------------

// How often the sibling list is re-read while a relay is configured. Slower
// than InstanceCard's own 3s stats poll, because this list only changes when
// another instance is switched on or off or the relay connection itself
// drops, not continuously the way a speed figure does.
const RELAY_POLL_MS = 5000;

/**
 * RelayCard is the "Vermittler" side of pairing: the address this instance
 * dials out to, the key that decides whose instances it meets there, and who
 * is currently on the other end of it.
 *
 * The two fields are saved on their own button rather than through the shared
 * settings draft the IdentityCard above uses, for the same reason PasswordCard
 * is: only the address is a setting. The key is a credential, it never travels
 * through GET/PUT /api/settings at all (internal/api/routes_relay.go's own
 * comment on why a secret must not ride on a route that hands the whole
 * document back), and the two halves are stored by one request that answers
 * with what is now stored - so what is rendered below is always the server's
 * account of it, never this form's hope.
 *
 * The key is write-only here. Once it is stored, this card can say that it is
 * and nothing more: the route never sends the plaintext back, not even
 * redacted, so there is nothing to display and no way to pretend otherwise.
 */
function RelayCard({ cx }: { cx: (k: PendingKey) => string }) {
  const { t } = useT();
  const [cfg, setCfg] = useState<RelayConfig | null>(null);
  const [url, setUrl] = useState('');
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  const [siblings, setSiblings] = useState<Instance[]>([]);

  useEffect(() => {
    fetchRelayConfig()
      .then((c) => {
        setCfg(c);
        setUrl(c.relayUrl);
      })
      .catch(() => setCfg(null));
  }, []);

  // A relay peer arrives on the ordinary peer list (GET /api/instances,
  // federation.Manager.List merges the stored peers and the relay-visible
  // ones into the one list the Instances page already draws) and is told
  // apart by carrying a relayId. Reading it here rather than inventing a
  // second endpoint keeps one answer to "who can this instance see".
  const live = !!cfg && cfg.relayUrl !== '' && cfg.keySet;
  useEffect(() => {
    if (!live) {
      setSiblings([]);
      return;
    }
    let alive = true;
    const load = () =>
      fetchInstances()
        .then((list) => {
          if (alive) setSiblings(list.filter((p) => !!p.relayId));
        })
        // A missed poll leaves the previous rows up rather than blanking a
        // list that was right a moment ago - the relay dropping out is
        // reported by the next successful read, not by a failed one.
        .catch(() => {});
    void load();
    const timer = window.setInterval(() => void load(), RELAY_POLL_MS);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [live]);

  // Hidden entirely when the route is not there: a build without the relay
  // has nothing to configure, and an empty form for a feature that cannot
  // work is worse than no card at all.
  if (!cfg) return null;

  // undefined leaves the stored key alone, '' clears it, anything else
  // replaces it - the three cases PUT /api/relay/config tells apart by
  // whether `key` is on the wire, which is why an untouched field must send
  // nothing rather than the empty string it holds.
  async function save(nextKey?: string) {
    setError('');
    setBusy(true);
    try {
      const c = await saveRelayConfig(url.trim(), nextKey);
      setCfg(c);
      setUrl(c.relayUrl);
      setKey('');
      setDone(true);
      setTimeout(() => setDone(false), 1800);
    } catch (e) {
      // The server's own sentence, unwrapped: json() throws an ApiError whose
      // name would otherwise be printed in front of it (String(err) reads
      // "ApiError: ..."), which is the route's message with noise on it.
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card className="flex flex-col gap-5">
      <SectionTitle hue={5} hint={`${cx('settings.access.relay.body')} ${cx('settings.access.relay.selfHosted')}`}>
        {cx('settings.access.relay.title')}
      </SectionTitle>

      <Field label={cx('settings.access.relay.urlLabel')} hint={cx('settings.access.relay.urlHint')}>
        <TextInput
          dir="ltr"
          spellCheck={false}
          placeholder={cx('settings.access.relay.urlPlaceholder')}
          value={url}
          onChange={(e) => setUrl(e.target.value)}
        />
      </Field>

      <Field label={cx('settings.access.relay.keyLabel')} hint={cx('settings.access.relay.keyHint')}>
        <TextInput
          type="password"
          dir="ltr"
          autoComplete="off"
          spellCheck={false}
          placeholder={cx(
            cfg.keySet ? 'settings.access.relay.keyPlaceholderSet' : 'settings.access.relay.keyPlaceholderUnset',
          )}
          value={key}
          onChange={(e) => setKey(e.target.value)}
        />
      </Field>

      <div className="flex items-center gap-3">
        <Button kind="secondary" disabled={busy} onClick={() => void save(key === '' ? undefined : key)}>
          {busy ? cx('settings.access.relay.saving') : cx('settings.access.relay.save')}
        </Button>
        <span className={`text-sm ${cfg.keySet ? 'text-statusOk' : 'text-carbon-textMuted'}`}>
          {cx(cfg.keySet ? 'settings.access.relay.keySet' : 'settings.access.relay.keyUnset')}
        </span>
        {cfg.keySet && (
          <IconBadge
            kind="danger"
            icon={<IconTrash width={15} height={15} />}
            disabled={busy}
            title={cx('settings.access.relay.keyClear')}
            aria-label={cx('settings.access.relay.keyClear')}
            onClick={() => void save('')}
          />
        )}
        <span className="flex-1" />
        {done && <span className="text-sm text-statusOk">{cx('settings.access.relay.saved')}</span>}
        {error && <span className="text-sm text-statusFail">{error}</span>}
      </div>

      <div className="flex flex-col gap-1.5">
        <span className="text-xs font-semibold text-carbon-textSub">{cx('settings.access.relay.siblingsTitle')}</span>
        {!live && <span className="text-[11px] text-carbon-textMuted">{cx('settings.access.relay.siblingsOff')}</span>}
        {live && siblings.length === 0 && (
          <span className="text-[11px] text-carbon-textMuted">{cx('settings.access.relay.siblingsEmpty')}</span>
        )}
        {siblings.map((p) => (
          <div key={p.relayId} className="flex items-center gap-2 text-sm">
            {/* Always the online dot: a relay peer is on this list exactly as
                long as the relay reports it connected, so there is no offline
                state for one to be in - it is simply gone from the next poll. */}
            <span
              role="img"
              aria-label={t('instances.online')}
              title={t('instances.online')}
              className="h-2 w-2 shrink-0 rounded-[var(--radius-pill)] bg-statusOkSolid"
            />
            <span className="min-w-0 flex-1 truncate text-carbon-text">{p.name}</span>
            <span className="glim-num max-w-[10rem] shrink-0 truncate text-[11px] text-carbon-textMuted" dir="ltr" title={p.relayId}>
              {p.relayId}
            </span>
          </div>
        ))}
      </div>
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
          hue={6}
          hint={cx('settings.access.tokens.intro')}
          right={
            <Button
              kind="secondary"
              hue={6}
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
                <IconBadge
                  kind="danger"
                  icon={<IconTrash width={15} height={15} />}
                  disabled={revoking === tok.id}
                  title={cx('settings.access.tokens.revoke')}
                  aria-label={cx('settings.access.tokens.revoke')}
                  onClick={() => void onRevoke(tok.id)}
                  className="shrink-0"
                />
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
            <div className="flex items-center gap-2">
              <div className="min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2">
                <code className="glim-num block overflow-x-auto whitespace-nowrap text-xs text-carbon-text" dir="ltr">
                  {created.secret}
                </code>
              </div>
              <IconBadge
                hue={6}
                icon={copied ? <IconCheck width={14} height={14} /> : <IconClipboard width={14} height={14} />}
                title={copied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                aria-label={copied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                onClick={async () => {
                  if (await copyToClipboard(created.secret)) {
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1800);
                  }
                }}
              />
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
