import { useEffect, useState } from 'react';
import {
  Button,
  Card,
  Field,
  IconBadge,
  InfoBubble,
  Modal,
  PasswordInput,
  SectionTitle,
  TextArea,
  TextInput,
} from '../../components/ui';
import { QRCode } from '../../components/QRCode';
import {
  type ApiToken,
  type AuthState,
  type NewApiToken,
  type ConnectInfo,
  type QRMatrix,
  type TsnetInfo,
  type TsnetPeer,
  activateConnect,
  addInstance,
  createToken,
  fetchAuth,
  fetchConnect,
  PhraseRejected,
  joinConnect,
  leaveConnect,
  revealConnect,
  fetchRemoteAccess,
  fetchTokens,
  fetchTsnetPeers,
  fetchTsnetStatus,
  logout,
  revokeToken,
  setPassword,
  startTsnet,
  stopTsnet,
} from '../../lib/api';
import { copyToClipboard } from '../../lib/clipboard';
import { fmtDate } from '../../lib/format';
import { useInstallPrompt } from '../../lib/pwaInstall';
import { useT, type TranslationKey } from '../../lib/i18n';
import {
  IconCheck,
  IconClipboard,
  IconClose,
  IconKey,
  IconPlus,
  IconSignOut,
  IconTrash,
  IconWarning,
} from '../../lib/icons';
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

  // settings.access.remote.title/combinedHint/desktopNote all retired
  // (jdp, 2026-08-27: merging the Tailscale card into this one too made the
  // section's own title and hint redundant with settings.access.tsnet.*
  // above it, and desktopNote had already gone unused since an earlier
  // redesign round - checked for real call sites before removing rather
  // than assumed dead) - see settings.access.tsnet.advancedTitle for the
  // disclosure label that replaces settings.access.remote.title's old job.
  'settings.access.remote.exposedWarning':
    'This instance just answered a request from outside this machine, and no password protects it. Anyone who can reach it can see and control every download. Set a password above now.',

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

  // installTitle/installBody/storeAndroid/storeIOS/storeComingSoon/
  // installPwaLabel are real, fully-translated locale keys now (jdp,
  // 2026-08-26 rescope: "die jetzige funktion die in der i infobubble
  // beschrieben ist will ich nicht... dort sollen die Verlinkungen zum Play
  // Store und App Store stehen") - not listed here, because installTitle
  // and installBody already existed in every locale file from an earlier
  // wave, and a PENDING entry here would have been silently shadowed by
  // that real value instead of overriding it (t(key) is checked before
  // PENDING[key], never the other way around) - the exact bug this file's
  // own comment about "not in en.ts yet" no longer describes for these six
  // keys, and almost caused this rescope to ship invisibly.
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
  // body/urlPlaceholder/urlHint/bothSidesHint/keyHint are real,
  // fully-translated locale keys now, rewritten (jdp, 2026-08-26: "Das ist
  // alles viel zu kompliziert! die infotexte sind verwirrend... Wo muss man
  // die relayadresse in der anderen instanz eingeben? ... Muss die
  // Relayadresse keine domain sein?") to state the two things a first-time
  // relay user actually needs and never found stated outright: the SAME
  // address+key goes into this same spot on every instance you connect,
  // and a domain is not required. Not listed here for the same shadowing
  // reason as the install keys above - see that comment. The card's shape
  // (one merged pairing+relay card, folded relay section) stays unchanged;
  // the problem measured out to be the copy, not the layout.
  'settings.access.relay.selfHosted':
    'Nobody runs a relay for you. It is a separate binary you host yourself, the same way you already host KnightLoader, and it only routes messages between your own instances: no download and no file byte ever travels over it. Leaving this empty changes nothing about the rest of this page.',
  'settings.access.relay.urlLabel': 'Relay address',
  'settings.access.relay.keyLabel': 'Relay key',
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
      {/* Identity first, password second (jdp, 2026-08-26) - and pulled out of
          the old RemoteAccessSection rather than left nested inside it: that
          section returned null until its own fetch resolved and skipped this
          card entirely on a desktop build. A name is a plain settings field
          with no such dependency - it belongs at the top regardless of
          deployment, not hidden behind a fetch that has nothing to do with
          it. */}
      <IdentityCard cx={cx} />
      <PasswordCard />

      {/* Only the exposed-warning banner now (jdp, 2026-08-26: "Die
          netzwerkzugriffcard entfernen wir. die ist völlig witzlos. auf der
          desktop version funktioniert das eh nicht") - NetworkAccessCard
          (this instance's own LAN address + QR) is gone, and with it the
          whole reason this used to be deployment-gated: nothing left in
          here depends on fetchRemoteAccess()'s deployment field at all. */}
      <ExposedWarningBanner cx={cx} />
      {/* GetTheAppCard is unconditional, unlike the old install card it
          replaces (which lived inside the now-removed deployment-gated
          section and never rendered on desktop) - which apps exist to
          install has nothing to do with network-reachability fetch state. */}
      <GetTheAppCard cx={cx} />
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
  const [signingOut, setSigningOut] = useState(false);

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
        {/* Status stays a visible, at-a-glance line rather than moving fully
            into the bubble (jdp, 2026-08-26: "in eine i infobubble und
            schöner beschreiben") - whether this instance is protected is
            worth seeing without hovering anything. Only the WHY (what a
            password actually guards against) moves into the title's own
            hint bubble, with nicer wording than the old lockOff sentence it
            replaces. */}
        <SectionTitle hue={0} hint={t('settings.lockHint')}>
          {t('auth.password')}
        </SectionTitle>
        <p className={`text-sm ${locked ? 'text-statusOk' : 'text-carbon-textSub'}`}>
          {locked ? t('settings.lockOn') : t('settings.lockOff')}
        </p>
        {locked && (
          <Field label={t('settings.passwordCurrent')}>
            <PasswordInput
              value={current}
              onChange={setCurrent}
              autoComplete="current-password"
              showLabel={t('common.showPassword')}
              hideLabel={t('common.hidePassword')}
            />
          </Field>
        )}
        <Field label={t('settings.passwordNew')} hint={t('settings.passwordHint')}>
          <PasswordInput
            value={next}
            onChange={setNext}
            autoComplete="new-password"
            showLabel={t('common.showPassword')}
            hideLabel={t('common.hidePassword')}
          />
        </Field>
        <div className="flex flex-wrap items-center gap-3">
          <Button kind="secondary" hue={0} onClick={onApply} disabled={locked ? current === '' : next === ''}>
            {next === '' && locked ? t('settings.removePassword') : t('settings.setPassword')}
          </Button>
          {/* Only once a password is actually protecting this instance -
              seeing this page at all already means the current session is
              authenticated, the same "locked" flag Sidebar.tsx's own sign-out
              entry reads (jdp, 2026-08-26: "Wenn man ein passwort gesetzt hat
              muss man sich doch auch auslogen können oder? dafür fehlt ein
              button" - right here on the card that sets the password, not
              only tucked into the sidebar). */}
          {locked && (
            <Button
              kind="ghost"
              icon={<IconSignOut width={15} height={15} />}
              disabled={signingOut}
              onClick={async () => {
                setSigningOut(true);
                try {
                  await logout();
                  location.reload();
                } catch (e) {
                  // A failed sign-out (offline, a server hiccup) must not
                  // leave the button looking clicked-but-dead with no
                  // explanation - the same gap Sidebar.tsx's own identical
                  // call has, not repeated silently here a second time.
                  setSigningOut(false);
                  setError(e instanceof Error ? e.message : String(e));
                }
              }}
            >
              {t('auth.signOut')}
            </Button>
          )}
          {done && <span className="text-statusOk text-sm">{t('settings.passwordSaved')}</span>}
          {error && <span className="text-statusFail text-sm">{error}</span>}
        </div>
      </Card>
  );
}

// ---- Exposed warning & getting the app --------------------------------------

// ExposedWarningBanner reads GET /api/remote-access once on mount purely for
// the `exposed` flag - whether this instance has ever answered a request
// from outside this machine while unprotected. Dismissible per tab (jdp,
// 2026-08-26: "Es steht die ganze zeit die meldung... kann man die nicht
// mit einem x button versehen um sie zu entfernen?") rather than
// permanently: the risk it names is real again on the next page load if
// still unprotected, so a dismissal that survived a reload would silence a
// warning about a problem that never actually got fixed.
function ExposedWarningBanner({ cx }: { cx: (k: PendingKey) => string }) {
  const { t } = useT();
  const [exposed, setExposed] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    fetchRemoteAccess()
      .then((info) => setExposed(info.exposed))
      .catch(() => setExposed(false));
  }, []);

  if (!exposed || dismissed) return null;

  return (
    <div className="flex items-start gap-3 rounded-[var(--radius-card)] bg-statusFailBg p-4">
      <IconWarning width={20} height={20} className="mt-0.5 shrink-0 text-statusFail" />
      <p className="min-w-0 flex-1 text-sm font-medium text-statusFail">
        {cx('settings.access.remote.exposedWarning')}
      </p>
      {/* A plain button in the banner's own red, not an IconBadge tile - a
          colourful square badge dropped onto a solid alert strip would read
          as an unrelated control rather than part of the same message. */}
      <button
        type="button"
        onClick={() => setDismissed(true)}
        title={t('common.dismiss')}
        aria-label={t('common.dismiss')}
        className="shrink-0 rounded-[var(--radius-control)] p-1 text-statusFail/70 transition-colors hover:bg-statusFail/10 hover:text-statusFail"
      >
        <IconClose width={14} height={14} />
      </button>
    </div>
  );
}

// GetTheAppCard leads with the native apps KnightLoader actually ships
// (jdp, 2026-08-26: "wir veröffentlichen ja desktop versionen und auch
// native Apps. Dort sollen die Verlinkungen zum Play Store und App Store
// stehen"), with installing this page itself (the old card's whole
// premise) kept as the smaller, still-real second option rather than the
// headline. Unlike the old install card it replaces - nested inside the
// now-removed RemoteAccessSection, gated by fetchRemoteAccess(), and
// invisible on a desktop deployment - this one renders unconditionally:
// which apps exist has nothing to do with network-reachability fetch
// state.
function GetTheAppCard({ cx }: { cx: (k: PendingKey) => string }) {
  // installTitle/installBody/installPwaLabel are real, translated locale
  // keys (see the PENDING map's own comment on why they are not listed
  // there) - read via t(), not cx(), same as install/installIOS just below
  // stay on cx() because those two are still PENDING-only.
  const { t } = useT();
  const { available: canInstall, promptInstall } = useInstallPrompt();
  // Safari (desktop and iOS) never fires beforeinstallprompt, so
  // useInstallPrompt's `available` is permanently false there - this is the
  // one place that still has something useful to say instead of nothing:
  // the manual Share-sheet route, iOS's only way to install any web app.
  const iOS = /iphone|ipad|ipod/i.test(navigator.userAgent);

  return (
    <Card className="flex flex-col gap-4">
      <SectionTitle hue={3} hint={t('settings.access.remote.installBody')}>
        {t('settings.access.remote.installTitle')}
      </SectionTitle>
      <div className="flex flex-wrap gap-3">
        <StoreButton store="android" />
        <StoreButton store="ios" />
      </div>
      {(canInstall || iOS) && (
        <div className="flex flex-col gap-2 border-t border-carbon-border/40 pt-4">
          <span className="text-xs font-semibold text-carbon-textSub">
            {t('settings.access.remote.installPwaLabel')}
          </span>
          {canInstall && (
            <div>
              <Button kind="secondary" hue={3} onClick={() => void promptInstall()}>
                {cx('settings.access.remote.install')}
              </Button>
            </div>
          )}
          {!canInstall && iOS && (
            <p className="text-[11px] text-carbon-textMuted">{cx('settings.access.remote.installIOS')}</p>
          )}
        </div>
      )}
    </Card>
  );
}

// Filled in once a listing actually goes live in that store - empty for now
// (mirrors BrowserTools.tsx's own STORE_URLS: submission needs a completed
// store listing, not only the developer account, which is already the
// state a native-app store submission is in). Until a URL is set here, the
// button stays disabled with an explanatory tooltip rather than guessing at
// a fallback destination: unlike the browser extension's packaged .zip/.xpi
// (downloadable straight from this instance), there is no equivalent
// direct-install file for a native mobile app to fall back to.
const APP_STORE_URLS: Record<'android' | 'ios', string> = {
  android: '',
  ios: '',
};

function StoreButton({ store }: { store: 'android' | 'ios' }) {
  const { t } = useT();
  const url = APP_STORE_URLS[store];
  const label = t(store === 'android' ? 'settings.access.remote.storeAndroid' : 'settings.access.remote.storeIOS');
  return (
    <Button
      kind="secondary"
      disabled={!url}
      title={url ? undefined : t('settings.access.remote.storeComingSoon')}
      onClick={() => url && window.open(url, '_blank', 'noopener,noreferrer')}
    >
      {label}
    </Button>
  );
}

// IdentityCard: an optional friendly name and the domains this instance is
// known to be reachable through, both read/written through the normal
// settings draft (useDraft) and the shared Save bar, same as any other field
// on this page - not a live-preview control like Look.tsx's, so there is no
// reason to save it any differently.
//
// KnownDomains is shown here for the SAME reason a bare LAN IP still shows in
// NetworkAccessCard's own addresses list below: this is a normal settings
// field, editable independently of whatever request happened to load this
// page. The list itself is otherwise populated automatically
// (routes_remote.go's own rememberDomain, the moment a request actually
// arrives on a domain) - this textarea only matters for a domain that is
// already configured but has not been visited through yet, so pairingSelf and
// the QR code below have something to prefer before that first visit
// happens.
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

// ---- Remote access (Tailscale/Funnel, primary + pairing/relay, advanced) -----

// How often this card re-polls GET /api/tsnet/status while "connecting":
// authUrl and, later, funnelUrl both arrive from a goroutine this page's own
// POST /api/tsnet/start already returned without waiting for (see
// tsnetsrv.Manager.run) - there is no push channel for either, so the page
// finds out the same way the relay siblings list just above does, by
// asking again.
const TSNET_POLL_MS = 2000;

// How often the discovered-peers list is re-read while connected - the same
// cadence as the relay siblings list, for the same reason: this changes
// only when another of the user's own instances logs into Tailscale or
// drops offline, not continuously.
const TSNET_PEERS_POLL_MS = 5000;

/**
 * RemoteAccessCard is the one place to connect this instance with another
 * KnightLoader you run yourself, or make it reachable from anywhere at all.
 * ONE card now, not three cards across two redesigns (jdp, 2026-08-25:
 * "das muss einfach ein Punkt sein nicht mehr" merged Pairing+Relay; jdp,
 * 2026-08-27, on finding a separate "Von überall erreichbar" card sitting
 * next to this one: "Wieso gibt es jetzt... zwei card? Es soll nur eine
 * geben?" merged that in too).
 *
 * The Tailscale/Funnel login (internal/tsnetsrv) is the PRIMARY path: one
 * login gives this instance a real public address AND - the discovery
 * section below - automatically finds this same person's other instances
 * the moment they are logged into the same Tailscale account, with no
 * pairing code and no relay key at all (jdp: "wie genau wird das jetzt
 * umgesetzt?" - tsnet's own LocalClient().Status() already lists every
 * other device on the same account, so the "instance-to-instance" job the
 * pairing/relay mechanism used to be the ONLY way to do is now a side
 * effect of the one login this card already asks for).
 *
 * Pairing and relay stay, folded into the "advanced" disclosure at the
 * bottom - the way to connect two instances WITHOUT any third party at
 * all, for someone who wants that specifically. Both still do very
 * different things under the hood - a pairing code is a direct, one-time
 * exchange between two instances that can already reach each other; a
 * relay is a self-hosted go-between for two that cannot - so they stay two
 * sections inside one disclosure rather than one merged form that would
 * have to paper over that difference.
 *
 * Pairing needs the other side to be able to complete the exchange back to
 * this instance (routes_pairing.go's own redeem handler calls back here). A
 * desktop build opens no port of its own, so for a long time that was
 * impossible there and the section was simply hidden - but the relay
 * carries the completion now, addressing this instance by identifier
 * rather than by address, so a desktop ON A RELAY can issue and redeem
 * codes like anything else. The section therefore follows the relay's
 * actual state rather than the deployment, which is the thing that was
 * really being asked about (fetchDeploymentInfo, not fetchRemoteAccess:
 * this card renders outside RemoteAccessSection specifically so it still
 * shows on desktop, see Access()'s own comment on why).
 */
function RemoteAccessCard({ cx }: { cx: (k: PendingKey, vars?: Record<string, string | number>) => string }) {
  const { t } = useT();

  // --- Connection phrase (instance to instance) ---
  //
  // Kept apart from the Tailscale state below on purpose: the two answer
  // different questions and can both be on. This one connects a person's
  // OWN instances to each other through the relay; Tailscale gives this
  // instance a public address a phone's browser can open. Merging their
  // state would make a card that cannot say which of the two is working.
  const [conn, setConn] = useState<ConnectInfo | null>(null);
  const [phrase, setPhrase] = useState('');
  // Kept beside the phrase rather than derived from it: the matrix is the
  // server's, and re-encoding it here would be a second implementation of
  // the same code to keep in step with the pairing one.
  const [phraseQr, setPhraseQr] = useState<QRMatrix | null>(null);
  const [phraseBusy, setPhraseBusy] = useState(false);
  const [phraseErr, setPhraseErr] = useState('');
  const [phraseCopied, setPhraseCopied] = useState(false);
  const [joinInput, setJoinInput] = useState('');
  const [joinOpen, setJoinOpen] = useState(false);
  const [revealPw, setRevealPw] = useState('');
  const [revealOpen, setRevealOpen] = useState(false);

  const loadConn = () => fetchConnect().then(setConn).catch(() => {});
  useEffect(() => {
    void loadConn();
  }, []);

  async function onActivate() {
    setPhraseErr('');
    setPhraseBusy(true);
    try {
      const r = await activateConnect();
      setPhrase(r.phrase);
      setPhraseQr(r.qr ?? null);
      setConn(r.info);
    } catch (e) {
      setPhraseErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPhraseBusy(false);
    }
  }

  async function onJoin() {
    setPhraseErr('');
    setPhraseBusy(true);
    try {
      setConn(await joinConnect(joinInput));
      setJoinInput('');
      setJoinOpen(false);
    } catch (e) {
      // A rejected phrase comes back as a reason plus its specifics, never
      // as a sentence: the server cannot know which language to write in.
      // Everything else here is already a message.
      if (e instanceof PhraseRejected) {
        setPhraseErr(
          e.reason === 'unknown_word'
            ? t('settings.access.phrase.errUnknownWord', { position: e.position, word: e.word })
            : e.reason === 'word_count'
              ? t('settings.access.phrase.errWordCount', { count: e.count, need: 12 })
              : t('settings.access.phrase.errChecksum'),
        );
      } else {
        setPhraseErr(e instanceof Error ? e.message : String(e));
      }
    } finally {
      setPhraseBusy(false);
    }
  }

  async function onReveal() {
    setPhraseErr('');
    setPhraseBusy(true);
    try {
      const r = await revealConnect(revealPw);
      setPhrase(r.phrase);
      setPhraseQr(r.qr ?? null);
      setRevealPw('');
      setRevealOpen(false);
    } catch (e) {
      setPhraseErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPhraseBusy(false);
    }
  }

  async function onLeave() {
    setPhraseErr('');
    setPhraseBusy(true);
    try {
      await leaveConnect();
      // Cleared here rather than left for the reload: a phrase still on
      // screen after "leave" reads as though nothing happened.
      setPhrase('');
      setPhraseQr(null);
      await loadConn();
    } catch (e) {
      setPhraseErr(e instanceof Error ? e.message : String(e));
    } finally {
      setPhraseBusy(false);
    }
  }

  // --- Tailscale/Funnel (a public address for browsers) ---
  const [tsInfo, setTsInfo] = useState<TsnetInfo | null>(null);
  const [tsHostname, setTsHostname] = useState('');
  const [tsBusy, setTsBusy] = useState(false);
  const [tsErr, setTsErr] = useState('');
  const [tsQr, setTsQr] = useState<QRMatrix | null>(null);
  const [tsCopied, setTsCopied] = useState(false);
  const [peers, setPeers] = useState<TsnetPeer[]>([]);
  // The URL currently being added, so only THAT row's button shows busy -
  // adding one discovered peer must not disable every other row's button
  // while its own request is in flight.
  const [addingPeer, setAddingPeer] = useState('');
  const [peerErr, setPeerErr] = useState('');

  const loadTs = () => fetchTsnetStatus().then(setTsInfo).catch(() => {});
  useEffect(() => {
    void loadTs();
  }, []);

  useEffect(() => {
    if (tsInfo?.status !== 'connecting') return;
    const timer = window.setInterval(() => void loadTs(), TSNET_POLL_MS);
    return () => window.clearInterval(timer);
  }, [tsInfo?.status]);

  // The QR is the same one the pairing flow below would now compute
  // (preferredAddress ranks a connected funnel address ahead of everything
  // but the exact address this browser tab is already on) - fetched here
  // rather than duplicated, so a person who never opens "Code anzeigen"
  // still gets a scannable code the moment this card itself connects.
  useEffect(() => {
    if (tsInfo?.status !== 'connected') {
      setTsQr(null);
      return;
    }
    fetchRemoteAccess()
      .then((r) => setTsQr(r.qr ?? null))
      .catch(() => setTsQr(null));
  }, [tsInfo?.status]);

  // Discovered peers: only while connected, and only this instance's own
  // other devices (the server already filters to the same Tailscale
  // account and to devices that answer like KnightLoader - see
  // tsnetsrv.Manager.Peers' own doc comment).
  useEffect(() => {
    if (tsInfo?.status !== 'connected') {
      setPeers([]);
      return;
    }
    let alive = true;
    const load = () =>
      fetchTsnetPeers()
        .then((p) => {
          if (alive) setPeers(p);
        })
        .catch(() => {});
    void load();
    const timer = window.setInterval(() => void load(), TSNET_PEERS_POLL_MS);
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [tsInfo?.status]);

  async function onTsConnect() {
    setTsErr('');
    setTsBusy(true);
    try {
      setTsInfo(await startTsnet(tsHostname.trim()));
    } catch (e) {
      setTsErr(e instanceof Error ? e.message : String(e));
    } finally {
      setTsBusy(false);
    }
  }

  async function onTsDisconnect() {
    setTsBusy(true);
    try {
      setTsInfo(await stopTsnet());
    } finally {
      setTsBusy(false);
    }
  }

  // Same shape as Instances.tsx's own onAddFound for a discovered LAN
  // instance - "refused us" (password set) and "could not be reached" get
  // the same two distinct, already-translated sentences that page already
  // uses, not new copy for what is the same underlying situation.
  async function onAddPeer(p: TsnetPeer) {
    setPeerErr('');
    setAddingPeer(p.url);
    try {
      const r = await addInstance(p.hostname, p.url);
      if (r.needsPairing) setPeerErr(t('instances.needsPairing'));
      else if (!r.online) setPeerErr(t('instances.offlineWarning'));
      // Removed from the candidate list either way: a successful add has
      // nothing left to offer, and a refused/offline one is still "known"
      // now (federation stores it regardless of whether it answered), so
      // the next poll's own server-side filter would drop it anyway - this
      // just does not wait for that poll to say so.
      setPeers((cur) => cur.filter((x) => x.url !== p.url));
    } catch (e) {
      setPeerErr(e instanceof Error ? e.message : String(e));
    } finally {
      setAddingPeer('');
    }
  }

  // Held until Tailscale has answered: an empty card for a feature that
  // has not reported yet flickers into place a moment later, which reads as
  // a bug. This is a loading guard, not a capability check - the route is
  // always registered.
  if (!tsInfo) return null;

  return (
    <Card className="flex flex-col gap-5">
      <SectionTitle hue={1} hint={t('settings.access.phrase.body')}>
        {t('settings.access.cardTitle')}
      </SectionTitle>

      {/* Nothing set up yet: two ways in, and they are the two ends of the
          same act - start a group, or join one somebody already started. */}
      {conn && !conn.active && (
        <div className="flex flex-col gap-3">
          {/* jdp's call (2026-08-27): warn loudly, do not block. The
              sentence has to carry the part that is not obvious - that this
              phrase reaches every instance in the group, so an unprotected
              instance puts the others at risk too, not only itself. */}
          {!conn.passwordSet && (
            <div className="flex items-start gap-3 rounded-[var(--radius-card)] bg-statusFailBg p-4">
              <IconWarning width={20} height={20} className="mt-0.5 shrink-0 text-statusFail" />
              <p className="min-w-0 flex-1 text-sm font-medium text-statusFail">
                {t('settings.access.phrase.noPasswordWarning')}
              </p>
            </div>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <Button hue={1} disabled={phraseBusy} onClick={() => void onActivate()}>
              {t('settings.access.phrase.activate')}
            </Button>
            <Button
              kind="secondary"
              hue={1}
              icon={<IconClipboard width={14} height={14} />}
              onClick={() => {
                setJoinOpen(!joinOpen);
                setPhraseErr('');
              }}
            >
              {t('settings.access.phrase.joinButton')}
            </Button>
          </div>
          {joinOpen && (
            <div className="flex flex-col gap-2 sm:flex-row">
              <TextInput
                dir="ltr"
                spellCheck={false}
                className="min-w-0 flex-1"
                placeholder={t('settings.access.phrase.joinPlaceholder')}
                value={joinInput}
                onChange={(e) => setJoinInput(e.target.value)}
              />
              <Button hue={1} disabled={phraseBusy || joinInput.trim() === ''} onClick={() => void onJoin()}>
                {t('settings.access.phrase.joinConfirm')}
              </Button>
            </div>
          )}
        </div>
      )}

      {/* Set up: say whether it is actually working, offer the phrase, and
          allow leaving. "Stored" and "connected" are shown separately
          because a phrase with an unreachable relay is configured but not
          working, and one word for both is what made the old relay card
          unable to say which had gone wrong. */}
      {conn?.active && (
        <div className="flex flex-col gap-3">
          <div className="flex items-start gap-2.5">
            <span
              className={`mt-1.5 h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${
                conn.connected ? 'bg-statusOkSolid' : 'bg-carbon-textMuted'
              }`}
            />
            <span className="min-w-0 flex-1 text-sm text-carbon-text">
              {conn.connected ? t('settings.access.phrase.stateConnected') : t('settings.access.phrase.stateConnecting')}
            </span>
          </div>

          {phrase ? (
            <div className="flex flex-col gap-2">
              <span className="text-xs font-semibold text-carbon-textSub">
                {t('settings.access.phrase.yourPhrase')}
              </span>
              <div className="flex items-start gap-2">
                <code
                  className="glim-num min-w-0 flex-1 rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs leading-relaxed text-carbon-text"
                  dir="ltr"
                >
                  {phrase}
                </code>
                <IconBadge
                  hue={1}
                  icon={phraseCopied ? <IconCheck width={14} height={14} /> : <IconClipboard width={14} height={14} />}
                  title={phraseCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                  aria-label={phraseCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                  onClick={async () => {
                    if (await copyToClipboard(phrase)) {
                      setPhraseCopied(true);
                      setTimeout(() => setPhraseCopied(false), 1800);
                    }
                  }}
                />
              </div>
              <p className="text-[11px] text-carbon-textMuted">{t('settings.access.phrase.pasteHint')}</p>
              {/* The QR is for the case the words are worst at: typing twelve
                  of them into a phone. Same component the pairing code uses,
                  and it is absent rather than broken when the server could
                  not encode one. */}
              {phraseQr && (
                <div className="flex flex-col items-center gap-1.5 pt-1">
                  <QRCode matrix={phraseQr} label={phrase} size={144} />
                  <span className="text-[11px] text-carbon-textMuted">{t('settings.access.phrase.qrHint')}</span>
                </div>
              )}
            </div>
          ) : revealOpen ? (
            <div className="flex flex-col gap-2">
              <p className="text-[11px] text-carbon-textMuted">{t('settings.access.phrase.revealWhy')}</p>
              <div className="flex flex-col gap-2 sm:flex-row">
                <div className="min-w-0 flex-1">
                  <PasswordInput
                    value={revealPw}
                    onChange={setRevealPw}
                    autoComplete="current-password"
                    showLabel={t('common.showPassword')}
                    hideLabel={t('common.hidePassword')}
                  />
                </div>
                <Button hue={1} disabled={phraseBusy} onClick={() => void onReveal()}>
                  {t('settings.access.phrase.revealConfirm')}
                </Button>
              </div>
            </div>
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                kind="secondary"
                hue={1}
                onClick={() => {
                  setPhraseErr('');
                  // With no password there is nothing to re-enter, so this
                  // goes straight to the answer instead of showing an empty
                  // field somebody has to press past.
                  if (conn.passwordSet) setRevealOpen(true);
                  else void onReveal();
                }}
              >
                {t('settings.access.phrase.showAgain')}
              </Button>
              <Button kind="ghost" disabled={phraseBusy} onClick={() => void onLeave()}>
                {t('settings.access.phrase.leave')}
              </Button>
            </div>
          )}
        </div>
      )}

      {phraseErr && <p className="text-sm text-statusFail">{phraseErr}</p>}

      {/* Remote access, below the phrase and clearly its own thing: it
          answers a different question (a public address a browser can open)
          rather than being an alternative to the phrase above. The card's
          own title names both halves, and these are them.

          A SUBDUED heading, not a second SectionTitle badge. Two filled
          badges in one card read as two cards glued together - which is the
          complaint that merged them in the first place - and SectionTitle's
          badge is absolutely positioned to straddle the CARD's edge, so a
          second one lands on top of the first anyway. */}
      <div className="flex flex-col gap-4 border-t border-carbon-border/40 pt-4">
      <div className="flex items-center gap-2">
        <span className="text-xs font-semibold text-carbon-textSub">
          {t('settings.access.remoteTitle')}
        </span>
        <InfoBubble tip={t('settings.access.tsnet.body')} />
      </div>

      {tsInfo.status === 'off' && (
        <div className="flex flex-col gap-3">
          <Field label={t('settings.access.tsnet.hostnameLabel')} hint={t('settings.access.tsnet.hostnameHint')}>
            <TextInput
              dir="ltr"
              spellCheck={false}
              placeholder={t('settings.access.tsnet.hostnamePlaceholder')}
              value={tsHostname}
              onChange={(e) => setTsHostname(e.target.value)}
            />
          </Field>
          <div>
            <Button hue={1} disabled={tsBusy} onClick={() => void onTsConnect()}>
              {tsBusy ? t('settings.access.tsnet.connecting') : t('settings.access.tsnet.connect')}
            </Button>
          </div>
        </div>
      )}

      {tsInfo.status === 'connecting' && (
        <div className="flex flex-col gap-2">
          {tsInfo.authUrl ? (
            <>
              <p className="text-sm text-carbon-text">{t('settings.access.tsnet.loginPrompt')}</p>
              <div>
                <Button hue={1} onClick={() => window.open(tsInfo.authUrl, '_blank', 'noopener,noreferrer')}>
                  {t('settings.access.tsnet.openLogin')}
                </Button>
              </div>
            </>
          ) : (
            <p className="text-sm text-carbon-textMuted">{t('settings.access.tsnet.connecting')}</p>
          )}
        </div>
      )}

      {tsInfo.status === 'connected' && (
        <div className="flex flex-col gap-3 sm:flex-row">
          <div className="flex min-w-0 flex-1 flex-col gap-3">
            <div className="flex flex-col gap-1">
              <span className="text-xs font-semibold text-carbon-textSub">
                {t('settings.access.tsnet.connectedLabel')}
              </span>
              <div className="flex items-center gap-2">
                <code
                  className="glim-num min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-xs text-carbon-text"
                  dir="ltr"
                >
                  {tsInfo.funnelUrl}
                </code>
                <IconBadge
                  hue={1}
                  icon={tsCopied ? <IconCheck width={14} height={14} /> : <IconClipboard width={14} height={14} />}
                  title={tsCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                  aria-label={tsCopied ? cx('settings.access.tokens.copied') : cx('settings.access.tokens.copy')}
                  onClick={async () => {
                    if (tsInfo.funnelUrl && (await copyToClipboard(tsInfo.funnelUrl))) {
                      setTsCopied(true);
                      setTimeout(() => setTsCopied(false), 1800);
                    }
                  }}
                />
              </div>
            </div>
            <div>
              <Button kind="secondary" hue={1} disabled={tsBusy} onClick={() => void onTsDisconnect()}>
                {tsBusy ? t('settings.access.tsnet.disconnecting') : t('settings.access.tsnet.disconnect')}
              </Button>
            </div>
          </div>
          {tsQr && tsInfo.funnelUrl && (
            <div className="flex shrink-0 flex-col items-center gap-2 self-start">
              <QRCode matrix={tsQr} label={tsInfo.funnelUrl} size={144} />
            </div>
          )}
        </div>
      )}

      {tsInfo.status === 'error' && (
        <div className="flex flex-col gap-2">
          <p className="text-sm text-statusFail">{tsInfo.error}</p>
          {tsInfo.error?.includes('Funnel') && (
            <p className="text-[11px] text-carbon-textMuted">{t('settings.access.tsnet.funnelErrorHint')}</p>
          )}
          <div>
            <Button hue={1} disabled={tsBusy} onClick={() => void onTsConnect()}>
              {tsBusy ? t('settings.access.tsnet.connecting') : t('settings.access.tsnet.connect')}
            </Button>
          </div>
        </div>
      )}

      {tsErr && <p className="text-sm text-statusFail">{tsErr}</p>}

      {/* Automatic instance-to-instance discovery, the direct answer to "wie
          genau wird das jetzt umgesetzt" - no pairing code, no relay key,
          because both instances already share the one login above. Shown
          only once there is something to offer: a person with no other
          instance sees nothing here, not an empty list. */}
      {tsInfo.status === 'connected' && peers.length > 0 && (
        <div className="flex flex-col gap-2 border-t border-carbon-border/40 pt-4">
          <span className="text-xs font-semibold text-carbon-textSub">
            {t('settings.access.tsnet.peersTitle')}
          </span>
          {peers.map((p) => (
            <div key={p.url} className="flex flex-wrap items-center gap-3">
              <span className="min-w-0 flex-1">
                <span className="text-sm text-carbon-text">{p.hostname}</span>
                <span className="ml-2 text-xs text-carbon-textMuted" dir="ltr">
                  {p.url}
                </span>
              </span>
              <Button
                kind="secondary"
                hue={1}
                className="px-2.5 text-xs"
                disabled={addingPeer === p.url}
                onClick={() => void onAddPeer(p)}
              >
                {t('instances.foundAdd')}
              </Button>
            </div>
          ))}
          {peerErr && <p className="text-sm text-statusFail">{peerErr}</p>}
        </div>
      )}
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
                  hue={5}
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
