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
  fetchDeploymentInfo,
  fetchInstances,
  fetchRelayConfig,
  fetchRemoteAccess,
  fetchTokens,
  generatePairingCode,
  redeemPairingCode,
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

  // settings.access.remote.title keeps its old text ("Remote access") but now
  // titles the MERGED pairing+relay card below (jdp, 2026-08-25: "das muss
  // einfach ein Punkt sein nicht mehr" - Pairing and Relay used to be two
  // separate cards for the same underlying job, connecting this instance
  // with another one you run). The card that used to own this title - this
  // instance's own address and a QR code to open it on another device on
  // the SAME network - was never actually "remote" in that sense (jdp: "der
  // jetztige Fernzugriff ist kein Fernzugriff, das ist nur netzwerk
  // intern"), and gets the new settings.access.network.* keys below instead.
  'settings.access.remote.title': 'Remote access',
  'settings.access.remote.desktopNote':
    'This is the desktop build. It does not serve the API over the network at all, so there is nothing here to reach from outside this application.',
  'settings.access.remote.exposedWarning':
    'This instance just answered a request from outside this machine, and no password protects it. Anyone who can reach it can see and control every download. Set a password above now.',
  'settings.access.remote.combinedHint':
    'Connect this instance with another KnightLoader you run yourself, so the two show up on each other’s Instances page. A pairing code is the quick way, for two instances that can already reach each other directly. A relay is for two that cannot - each behind its own NAT, on different networks.',

  'settings.access.network.title': 'Network access',
  'settings.access.network.desktopNote':
    'This is the desktop build. It does not serve the API over the network at all, so there is no address here to open on another device.',
  'settings.access.network.hint':
    "Open this instance's own interface on another device on the same network - a phone, another browser. This is not for connecting two KnightLoaders together; that is the Remote access card below.",
  'settings.access.network.addressesTitle': 'Addresses this instance answers on',
  'settings.access.network.noAddresses': 'No address could be determined for this request.',
  'settings.access.network.loopback': 'this machine only',
  'settings.access.network.domain': 'domain',
  'settings.access.network.showQr': 'Show QR code',
  'settings.access.network.hideQr': 'Hide QR code',
  'settings.access.network.scanHint': 'Only works on the same network as this instance.',

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
  'settings.access.remote.pairExpires': 'Valid for {min} minutes, then it expires unused.',
  'settings.access.remote.pairWhere': 'Paste it into the other instance, using its own “Enter a code” button.',
  'settings.access.remote.pairScan': 'Scan the QR code with the KnightLoader app.',

  // One sentence for "can another KnightLoader reach this one", because which
  // road it takes is this card's business and not the reader's. The four cases
  // are written out rather than assembled from clauses: a sentence stitched
  // together at runtime reads like one in every language it was written in.
  'settings.access.remote.stateLan': 'Another KnightLoader on this network can reach this one.',
  'settings.access.remote.stateBoth':
    'Another KnightLoader can reach this one on this network, and from anywhere through your relay.',
  'settings.access.remote.stateRelay': 'Another KnightLoader can reach this one from anywhere through your relay.',
  'settings.access.remote.stateNone':
    'Nothing can reach this one yet. This is the desktop build, which serves nothing over the network, so a relay is the way to connect it.',
  'settings.access.remote.showCode': 'Show a code',
  'settings.access.remote.hideCode': 'Hide the code',
  'settings.access.remote.enterCode': 'Enter a code',
  'settings.access.remote.beyond': 'Reaching instances outside this network',
  'settings.access.remote.beyondOff': 'not set up',

  'settings.access.relay.serveLabel': 'Run the relay on this instance',
  'settings.access.relay.serveHint':
    'Saves running a second program: the relay answers under /relay/connect on the address this instance already uses, behind the same reverse proxy and the same certificate, and the other instances point at that address. It admits only the key below, so nobody else can meet on it. What it cannot change is that a relay has to be reachable by both sides - turning this on inside an instance nothing can reach from outside gives the others nothing to dial.',
  'settings.access.relay.serveAddress': 'Give the other instances this address',
  'settings.access.relay.serveClients': 'connected right now: {n}',
  'settings.access.relay.serveOn': 'running here',
  'settings.access.relay.serveNeedsKey':
    'No relay key is stored, so nothing can connect to this relay yet. Set one below, and give the other instances the same one.',

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
    'Nothing right now - the relay is reachable, no other instance is connected with this key.',
  'settings.access.relay.unreachable':
    'The relay cannot be reached with this address and key. Check both, and that the relay is running - nothing else on this instance is affected.',

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
          card right after that section's own cards: that section renders
          nothing but a note on the desktop build, and the desktop build is
          the one that needs this card MOST - it opens no port at all, so a
          relay is its only way to be paired with anything. */}
      <RemoteAccessCard cx={cx} />
      <TokensSection cx={cx} />

      {listeners.length > 0 && (
          <Card className="flex flex-col gap-4">
            <SectionTitle hue={6} hint={cx('settings.access.intakePortsHint')}>
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
          <SectionTitle hue={1} hint={cx('settings.access.network.desktopNote')}>
            {cx('settings.access.network.title')}
          </SectionTitle>
        </Card>
    );
  }

  // Safari (desktop and iOS) never fires beforeinstallprompt, so
  // useInstallPrompt's `available` is permanently false there - this is the
  // one place that still has something useful to say instead of nothing:
  // the manual Share-sheet route, iOS's only way to install any web app.
  const iOS = /iphone|ipad|ipod/i.test(navigator.userAgent);

  return (
    <>
      {info.exposed && (
        <div className="flex items-start gap-3 rounded-[var(--radius-card)] bg-statusFailBg p-4">
          <IconWarning width={20} height={20} className="mt-0.5 shrink-0 text-statusFail" />
          <p className="text-sm font-medium text-statusFail">{cx('settings.access.remote.exposedWarning')}</p>
        </div>
      )}

      <NetworkAccessCard cx={cx} info={info} />

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
    </>
  );
}

// NetworkAccessCard is this instance's own address and a QR code to open its
// interface on another device on the SAME network (a phone, another
// browser) - not a way to connect two KnightLoaders together, which is
// RemoteAccessCard below (jdp, 2026-08-25: "der jetztige Fernzugriff ist
// kein Fernzugriff, das ist nur netzwerk intern").
//
// The QR is generated on request rather than drawn the moment this card
// mounts (jdp: "Dern QR code im jetzigen Fernzugriff bitte nur per Button
// anzeigen lassen, also ihn generieren wenn man es per button möchte") -
// info.qr already carries the matrix from the one GET this section already
// made, so "generating" it is just revealing what is already in memory, not
// a second request.
function NetworkAccessCard({ cx, info }: { cx: (k: PendingKey) => string; info: RemoteAccessInfo }) {
  const [showQr, setShowQr] = useState(false);
  const primary = info.addresses[0];

  return (
    <Card className="flex flex-col gap-4 sm:flex-row">
      <SectionTitle hue={1} hint={cx('settings.access.network.hint')}>
        {cx('settings.access.network.title')}
      </SectionTitle>
      <div className="flex min-w-0 flex-1 flex-col gap-3">
        <div className="flex flex-col gap-1.5">
          <span className="text-xs font-semibold text-carbon-textSub">
            {cx('settings.access.network.addressesTitle')}
          </span>
          {info.addresses.length === 0 && (
            <span className="text-[11px] text-carbon-textMuted">{cx('settings.access.network.noAddresses')}</span>
          )}
          {info.addresses.map((a) => (
            <div key={a.url} className="flex items-center gap-2 text-sm">
              <span className="glim-num min-w-0 flex-1 truncate text-carbon-text" dir="ltr">
                {a.url}
              </span>
              {a.loopback && (
                <span className="shrink-0 text-[11px] text-carbon-textMuted">
                  {cx('settings.access.network.loopback')}
                </span>
              )}
              {a.domain && (
                <span className="shrink-0 rounded-full bg-carbon-surface2 px-2 py-0.5 text-[11px] text-carbon-textSub">
                  {cx('settings.access.network.domain')}
                </span>
              )}
            </div>
          ))}
        </div>
        {info.qr && primary && (
          <div>
            <Button kind="secondary" hue={1} className="px-2.5 text-xs" onClick={() => setShowQr((v) => !v)}>
              {cx(showQr ? 'settings.access.network.hideQr' : 'settings.access.network.showQr')}
            </Button>
          </div>
        )}
      </div>
      {info.qr && primary && showQr && (
        <div className="flex shrink-0 flex-col items-center gap-2 self-start">
          <QRCode matrix={info.qr} label={primary.url} size={144} />
          <span className="max-w-[144px] text-center text-[11px] text-carbon-textMuted">
            {cx('settings.access.network.scanHint')}
          </span>
        </div>
      )}
    </Card>
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

// ---- Remote access (pairing + relay) -----------------------------------------

// How often the relay's sibling list is re-read while one is configured.
// Slower than InstanceCard's own 3s stats poll, because this list only
// changes when another instance is switched on or off or the relay
// connection itself drops, not continuously the way a speed figure does.
const RELAY_POLL_MS = 5000;

/**
 * RemoteAccessCard is the one place to connect this instance with another
 * KnightLoader you run yourself - merged from what used to be two separate
 * cards, Pairing and Relay (jdp, 2026-08-25: "das ist für einen
 * unerfahrenen user zu viel und zu kompliziert... das muss einfach ein
 * Punkt sein nicht mehr"). Both still do very different things under the
 * hood - a pairing code is a direct, one-time exchange between two
 * instances that can already reach each other; a relay is a self-hosted
 * go-between for two that cannot - so they stay two sections inside one
 * card rather than one merged form that would have to paper over that
 * difference.
 *
 * Pairing needs the other side to be able to complete the exchange back to
 * this instance (routes_pairing.go's own redeem handler calls back here). A
 * desktop build opens no port, so for a long time that was impossible there
 * and the section was simply hidden - but the relay carries the completion
 * now, addressing this instance by identifier rather than by address, so a
 * desktop ON A RELAY can issue and redeem codes like anything else. The
 * section therefore follows the relay's actual state rather than the
 * deployment, which is the thing that was really being asked about (fetchDeploymentInfo, not
 * fetchRemoteAccess: this card renders outside RemoteAccessSection
 * specifically so it still shows on desktop, see Access()'s own comment on
 * why).
 */
function RemoteAccessCard({ cx }: { cx: (k: PendingKey, vars?: Record<string, string | number>) => string }) {
  const { t } = useT();
  const [desktop, setDesktop] = useState(false);
  useEffect(() => {
    fetchDeploymentInfo()
      .then((d) => setDesktop(d.deployment === 'desktop'))
      .catch(() => {});
  }, []);

  // --- Connecting ---
  //
  // Both halves of it, in one place. Showing a code and entering one are the
  // same act seen from the two ends, and splitting them across two pages made
  // a person hold in their head which end they were at. Only one panel is open
  // at a time: they are alternatives, never a sequence.
  const [panel, setPanel] = useState<'' | 'show' | 'enter'>('');
  const [code, setCode] = useState<PairingCode | null>(null);
  const [pairBusy, setPairBusy] = useState(false);
  const [pairCopied, setPairCopied] = useState(false);
  const [pairErr, setPairErr] = useState('');

  const [redeem, setRedeem] = useState('');
  const [redeemBusy, setRedeemBusy] = useState(false);
  const [redeemMsg, setRedeemMsg] = useState('');
  const [redeemErr, setRedeemErr] = useState('');

  async function onGenerate() {
    setPairErr('');
    setPairBusy(true);
    setPanel('show');
    try {
      setCode(await generatePairingCode());
      setPairCopied(false);
    } catch (e: any) {
      setPairErr(String(e?.message ?? e));
    } finally {
      setPairBusy(false);
    }
  }

  async function onRedeem() {
    setRedeemErr('');
    setRedeemMsg('');
    setRedeemBusy(true);
    try {
      const r = await redeemPairingCode(redeem.trim());
      // The same three cases the Instances page tells apart, for the same
      // reason: the two directions fail separately, and a pairing that works
      // one way only is not a success with a footnote.
      const warn = !r.online && !r.reachedBack
        ? t('instances.pairNeitherWay')
        : !r.online
          ? t('instances.offlineWarning')
          : !r.reachedBack
            ? t('instances.pairOneWay')
            : '';
      const how = r.viaRelay ? ` ${t('instances.pairViaRelay')}` : '';
      setRedeemMsg(t('instances.pairSuccess', { name: r.name }) + how + (warn ? ` ${warn}` : ''));
      setRedeem('');
    } catch (e: any) {
      setRedeemErr(String(e?.message ?? e));
    } finally {
      setRedeemBusy(false);
    }
  }

  // --- Relay ---
  const [cfg, setCfg] = useState<RelayConfig | null>(null);
  // Read by the pairing section above, which on a desktop build exists only
  // while this is true.
  const relayConnected = !!cfg?.connected;
  const [url, setUrl] = useState('');
  const [key, setKey] = useState('');
  const [relayBusy, setRelayBusy] = useState(false);
  const [relayDone, setRelayDone] = useState(false);
  const [relayError, setRelayError] = useState('');
  const [siblings, setSiblings] = useState<Instance[]>([]);
  // null means nobody has clicked yet, so the row can decide for itself.
  const [openRelay, setOpenRelay] = useState<boolean | null>(null);

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
  const relayLive = !!cfg && cfg.relayUrl !== '' && cfg.keySet;
  useEffect(() => {
    if (!relayLive) {
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
  }, [relayLive]);

  // undefined leaves the stored key alone, '' clears it, anything else
  // replaces it - the three cases PUT /api/relay/config tells apart by
  // whether `key` is on the wire, which is why an untouched field must send
  // nothing rather than the empty string it holds.
  //
  // nextServe is the switch, and passing it makes this a save of the switch
  // ALONE: it sends the address that is already stored rather than whatever is
  // in the field, and leaves the field alone afterwards. A toggle that quietly
  // committed a half-typed address somebody was still editing, or that wiped
  // that edit on the way back, would be a control doing two things.
  async function saveRelay(nextKey?: string, nextServe?: boolean) {
    const switchOnly = nextServe !== undefined;
    setRelayError('');
    setRelayBusy(true);
    try {
      const c = await saveRelayConfig(switchOnly ? (cfg?.relayUrl ?? '') : url.trim(), nextKey, nextServe);
      setCfg(c);
      if (!switchOnly) {
        setUrl(c.relayUrl);
        setKey('');
      }
      setRelayDone(true);
      setTimeout(() => setRelayDone(false), 1800);
    } catch (e) {
      // The server's own sentence, unwrapped: json() throws an ApiError whose
      // name would otherwise be printed in front of it (String(err) reads
      // "ApiError: ..."), which is the route's message with noise on it.
      setRelayError(e instanceof Error ? e.message : String(e));
    } finally {
      setRelayBusy(false);
    }
  }

  // Hidden entirely when the relay route is not there: an empty form for a
  // feature that cannot work is worse than no section at all. In practice
  // this only lasts as long as the one fetch above takes - the route is
  // always registered - so this is a loading guard, not a real capability
  // check.
  if (!cfg) return null;

  // What actually works right now, in one sentence. Which of the two roads
  // carries it is this card's business, not the reader's: the question a
  // person arrives with is whether another KnightLoader can reach this one.
  const relayOk = relayLive && cfg.connected;
  const relayBroken = relayLive && !cfg.connected;
  const stateKey: PendingKey = desktop
    ? relayOk
      ? 'settings.access.remote.stateRelay'
      : 'settings.access.remote.stateNone'
    : relayOk
      ? 'settings.access.remote.stateBoth'
      : 'settings.access.remote.stateLan';
  const reachable = !desktop || relayOk;
  // Opens itself when a relay is already configured, because hiding the state
  // of something you run is worse than one extra open row, and follows the
  // click from the first one onwards.
  const relayOpen = openRelay ?? relayLive;

  return (
    <Card className="flex flex-col gap-5">
      <SectionTitle hue={4} hint={cx('settings.access.remote.combinedHint')}>
        {cx('settings.access.remote.title')}
      </SectionTitle>

      <div className="flex items-start gap-2.5">
        <span
          className={`mt-1.5 h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${
            reachable ? 'bg-statusOkSolid' : 'bg-carbon-textMuted'
          }`}
        />
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-sm text-carbon-text">{cx(stateKey)}</span>
          {/* Configured but the socket is down: a typo in the address, a key
              the relay rejects, or a relay that is simply not running. Its own
              line even when the sentence above is a cheerful one, because
              something you set up is broken and that is worth saying whether
              or not another road happens to be open. */}
          {relayBroken && <span className="text-[11px] text-statusFail">{cx('settings.access.relay.unreachable')}</span>}
        </div>
      </div>

      {/* Not "is this a desktop": "is there a way for the other side to
          complete the exchange back to here". On a container that is its own
          address; on a desktop it is the relay, and only while the socket is
          actually up - a stored address and key with nothing connected cannot
          carry a pairing. Matches pairingSelf's own gate on the server, which
          answers 409 in exactly the cases this hides the buttons for. */}
      {(!desktop || relayConnected) && (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <Button
              kind="secondary"
              hue={4}
              icon={<IconPlus width={14} height={14} />}
              onClick={() => (panel === 'show' ? setPanel('') : void onGenerate())}
              disabled={pairBusy}
            >
              {cx(panel === 'show' ? 'settings.access.remote.hideCode' : 'settings.access.remote.showCode')}
            </Button>
            <Button
              kind="secondary"
              hue={4}
              icon={<IconClipboard width={14} height={14} />}
              onClick={() => {
                setPanel(panel === 'enter' ? '' : 'enter');
                setRedeemErr('');
                setRedeemMsg('');
              }}
            >
              {cx('settings.access.remote.enterCode')}
            </Button>
          </div>

          {panel === 'show' && (
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
                        icon={pairCopied ? <IconCheck width={14} height={14} /> : <IconClipboard width={14} height={14} />}
                        title={pairCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                        aria-label={pairCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                        onClick={async () => {
                          if (await copyToClipboard(code.code)) {
                            setPairCopied(true);
                            setTimeout(() => setPairCopied(false), 1800);
                          }
                        }}
                      />
                    </div>
                    <p className="text-[11px] text-carbon-textMuted">{cx('settings.access.remote.pairWhere')}</p>
                    <p className="text-[11px] text-carbon-textMuted">
                      {cx('settings.access.remote.pairExpires', { min: Math.round(code.expiresIn / 60) })}
                    </p>
                  </div>
                )}
                {pairErr && <p className="text-sm text-statusFail">{pairErr}</p>}
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
          )}

          {panel === 'enter' && (
            <div className="flex flex-col gap-2">
              <div className="flex flex-col gap-2 sm:flex-row">
                <TextInput
                  dir="ltr"
                  spellCheck={false}
                  className="min-w-0 flex-1"
                  placeholder={t('instances.pairPlaceholder')}
                  value={redeem}
                  onChange={(e) => setRedeem(e.target.value)}
                />
                <Button hue={4} disabled={redeemBusy || redeem.trim() === ''} onClick={() => void onRedeem()}>
                  {t('instances.pairButton')}
                </Button>
              </div>
              {redeemMsg && <p className="text-sm text-statusOk">{redeemMsg}</p>}
              {redeemErr && <p className="text-sm text-statusFail">{redeemErr}</p>}
            </div>
          )}
        </div>
      )}

      {/* The relay, demoted to what it is: the thing you set up so the two
          buttons above keep working when the other instance is not on this
          network. Named by what it is for rather than by what it is, and
          folded away, because a person who has one already read the state
          sentence at the top and a person who has none is not shopping for a
          protocol. */}
      <div className="flex flex-col gap-3 border-t border-carbon-border/40 pt-4">
        {/* The bubble is a SIBLING of the button, not a child of it: it is
            interactive itself, and an interactive control inside a button is
            both an accessibility problem and a click the outer button would
            swallow. */}
        <div className="flex items-center gap-2">
        <button
          type="button"
          className="flex flex-1 items-center gap-2 text-left"
          aria-expanded={relayOpen}
          onClick={() => setOpenRelay(!relayOpen)}
        >
          <span className="text-xs font-semibold text-carbon-textSub">{cx('settings.access.remote.beyond')}</span>
          {/* Two roles, and either one counts as set up: dialling somebody
              else's relay, or being the relay. An instance serving one while
              dialling none would otherwise have read "not set up" on the row
              summarising the thing it was doing. */}
          <span
            className={`text-[11px] ${
              cfg.serve && !cfg.keySet
                ? 'text-statusFail'
                : relayOk || cfg.serve
                  ? 'text-statusOk'
                  : 'text-carbon-textMuted'
            }`}
          >
            {relayOk
              ? t('instances.online')
              : cfg.serve
                ? // Switched on without a key is not "running": Admit refuses
                  // every key, so the relay is up and admits nobody. Saying
                  // "running here" there would be the row lying about the one
                  // thing it exists to report.
                  cx(cfg.keySet ? 'settings.access.relay.serveOn' : 'settings.access.relay.keyUnset')
                : cx('settings.access.remote.beyondOff')}
          </span>
          <span className="flex-1" />
          <span className="text-[11px] text-carbon-textMuted" aria-hidden="true">
            {relayOpen ? '−' : '+'}
          </span>
        </button>
        <InfoBubble tip={`${cx('settings.access.relay.body')} ${cx('settings.access.relay.selfHosted')}`} />
        </div>

        {relayOpen && (
          <>
            {/* jdp: "Können wir nicht das relay in KL integrieren? Also wenn
                jemand zb zwei desktop instanzen hat und die koppeln will, dass
                er dann in einer instanz das relay aktiveren kann?" It sits
                above the address field on purpose: an instance serving the
                relay is the answer to "which address do I put in the others",
                so finding it after filling that field in is finding it too
                late. */}
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <span className="text-sm text-carbon-text">{cx('settings.access.relay.serveLabel')}</span>
                <InfoBubble tip={cx('settings.access.relay.serveHint')} />
                <span className="flex-1" />
                {cfg.serve && (
                  <span className="text-[11px] text-carbon-textMuted">
                    {cx('settings.access.relay.serveClients', { n: cfg.serveClients })}
                  </span>
                )}
                <NeutralSwitch
                  on={cfg.serve}
                  disabled={relayBusy}
                  name={cx('settings.access.relay.serveLabel')}
                  onChange={(next) => void saveRelay(undefined, next)}
                />
              </div>
              {/* The switch alone does nothing without a key, because Admit
                  compares against the stored one and there is nothing to
                  compare with. Left unsaid, this is a feature that is on,
                  reachable, and silently refuses everybody. */}
              {cfg.serve && !cfg.keySet && (
                <span className="text-[11px] text-statusFail">{cx('settings.access.relay.serveNeedsKey')}</span>
              )}
              {cfg.serve && (
                <div className="flex flex-col gap-1">
                  <span className="text-[11px] text-carbon-textSub">{cx('settings.access.relay.serveAddress')}</span>
                  {/* The address this page was opened on, which is the one
                      that actually reached this instance - not a guess
                      assembled from a hostname, and not a stored field that
                      goes stale the first time a domain changes.
                      
                      Marked when it is a loopback address, because then it is
                      the one address that CANNOT be what the other instances
                      dial: it reaches this machine only. Handing it over
                      unmarked is the same blind spot that once put a loopback
                      address into the pairing QR code - an admin looking at
                      their own instance always sees 127.0.0.1, and the page
                      has no other way to know. */}
                  <div className="flex flex-wrap items-baseline gap-2">
                    <code
                      className="glim-num min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs text-carbon-text"
                      dir="ltr"
                    >
                      {location.origin}
                    </code>
                    {isLoopbackHost(location.hostname) && (
                      <span className="shrink-0 text-[11px] text-statusFail">
                        {cx('settings.access.network.loopback')}
                      </span>
                    )}
                  </div>
                </div>
              )}
            </div>

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
              <Button kind="secondary" disabled={relayBusy} onClick={() => void saveRelay(key === '' ? undefined : key)}>
                {relayBusy ? cx('settings.access.relay.saving') : cx('settings.access.relay.save')}
              </Button>
              <span className={`text-sm ${cfg.keySet ? 'text-statusOk' : 'text-carbon-textMuted'}`}>
                {cx(cfg.keySet ? 'settings.access.relay.keySet' : 'settings.access.relay.keyUnset')}
              </span>
              {cfg.keySet && (
                <IconBadge
                  kind="danger"
                  icon={<IconTrash width={15} height={15} />}
                  disabled={relayBusy}
                  title={cx('settings.access.relay.keyClear')}
                  aria-label={cx('settings.access.relay.keyClear')}
                  onClick={() => void saveRelay('')}
                />
              )}
              <span className="flex-1" />
              {relayDone && <span className="text-sm text-statusOk">{cx('settings.access.relay.saved')}</span>}
              {relayError && <span className="text-sm text-statusFail">{relayError}</span>}
            </div>

            <div className="flex flex-col gap-1.5">
              <span className="text-xs font-semibold text-carbon-textSub">
                {cx('settings.access.relay.siblingsTitle')}
              </span>
              {!relayLive && (
                <span className="text-[11px] text-carbon-textMuted">{cx('settings.access.relay.siblingsOff')}</span>
              )}
              {relayOk && siblings.length === 0 && (
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
                  <span className="min-w-0 flex-1 truncate text-carbon-text">{p.displayName ?? p.name}</span>
                  <span
                    className="glim-num max-w-[10rem] shrink-0 truncate text-[11px] text-carbon-textMuted"
                    dir="ltr"
                    title={p.relayId}
                  >
                    {p.relayId}
                  </span>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </Card>
  );
}

/**
 * isLoopbackHost reports whether a hostname only ever reaches the machine it is
 * typed on. Used to mark the address this card offers to hand to other
 * instances: a relay nobody else can dial is the one failure this feature can
 * produce silently, and an admin browsing their own instance sees 127.0.0.1
 * every time.
 *
 * IPv6 hostnames arrive from location.hostname without their brackets, so ::1
 * is compared bare.
 */
function isLoopbackHost(host: string): boolean {
  const h = host.toLowerCase();
  return h === 'localhost' || h === '::1' || h === '[::1]' || h.endsWith('.localhost') || /^127\./.test(h);
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
          hue={5}
          hint={cx('settings.access.tokens.intro')}
          right={
            <Button
              kind="secondary"
              hue={5}
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
                hue={5}
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
